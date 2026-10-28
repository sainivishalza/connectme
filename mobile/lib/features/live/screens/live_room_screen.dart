import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/network/api_client.dart';
import '../../../core/theme/tokens.dart';
import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../../payments/payments_repository.dart';
import '../live_repository.dart';
import '../media_session.dart';
import '../widgets/chat_panel.dart';
import '../widgets/gift_overlay.dart';
import '../widgets/live_poll_card.dart';
import '../widgets/stage_widgets.dart';
import '../widgets/room_theme.dart';
import '../../../core/realtime/live_refresh.dart';

/// Watch a stream: subscribe-only video, public chat and gifts.
class LiveRoomScreen extends ConsumerStatefulWidget {
  const LiveRoomScreen({
    super.key,
    required this.roomId,
    this.room,
    this.pollInterval = const Duration(seconds: 2),
  });
  final String roomId;
  final LiveRoom? room;
  final Duration pollInterval;

  @override
  ConsumerState<LiveRoomScreen> createState() => _LiveRoomScreenState();
}

class _LiveRoomScreenState extends ConsumerState<LiveRoomScreen> {
  late final LiveMedia _media;
  late final LiveRepository _repo;
  final _gifts = GiftOverlayController();
  final List<LiveEvent> _chat = [];
  LiveRefresh? _live;
  int _lastEvent = 0;
  bool _polling = false;
  Object? _error;
  bool _connected = false;
  bool _ended = false;
  String _pinned = '';
  List<String> _cohosts = [];
  LivePoll? _livePoll;
  StageState _stage = const StageState();
  bool _publishing = false;

  @override
  void initState() {
    super.initState();
    _repo = ref.read(liveRepositoryProvider);
    _media = ref.read(liveMediaFactoryProvider)();
    _connect();
  }

  Future<void> _connect() async {
    try {
      final info = await _repo.join(widget.roomId);
      await _media.connect(info.url, info.token, publish: info.canPublish);
      _publishing = info.canPublish;
      if (!mounted) return;
      setState(() => _connected = true);
      _live = LiveRefresh.of(ref, kind: 'live', id: widget.roomId, interval: widget.pollInterval, onTick: _poll).start();
      _poll();
    } catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  Future<void> _poll() async {
    if (_polling || !mounted) return;
    _polling = true;
    try {
      final page = await _repo.page(widget.roomId, after: _lastEvent);
      final events = page.events;
      if (!mounted) return;
      final np = page.poll;
      if (np?.id != _livePoll?.id || np?.counts.join(',') != _livePoll?.counts.join(',') || np?.closed != _livePoll?.closed || np?.myChoice != _livePoll?.myChoice) {
        setState(() => _livePoll = np);
      }
      if (page.pinned != _pinned || page.cohosts.join(',') != _cohosts.join(',')) {
        setState(() {
          _pinned = page.pinned;
          _cohosts = page.cohosts;
        });
      }
      if (page.stage != _stage) {
        setState(() => _stage = page.stage);
        _syncPublishing();
      }
      final first = _lastEvent == 0;
      for (final e in events) {
        _lastEvent = e.id > _lastEvent ? e.id : _lastEvent;
        if (e.isGift) {
          // Skip announcing the backlog the viewer joined after.
          if (!first) _gifts.show('${e.name} ${e.text}');
        } else {
          _chat.add(e);
        }
      }
      if (events.isNotEmpty) setState(() {});
    } on ApiException catch (e) {
      // 410/404: the stream ended or we lost access. Anything else (network,
      // rate limit) is transient and the next tick retries.
      if (mounted && (e.status == 410 || e.status == 404)) {
        _live?.dispose();
        setState(() => _ended = true);
      }
    } catch (_) {
      // transient; retried on the next tick
    } finally {
      _polling = false;
    }
  }

  /// Turns the camera and microphone on when the host lets this viewer on
  /// stage, and off again when they step down or are removed.
  Future<void> _syncPublishing() async {
    final media = _media;
    if (media is! StageMedia) return;
    final want = _stage.onStage;
    if (want == _publishing) return;
    _publishing = want;
    try {
      await (media as StageMedia).setPublishing(want);
      if (want && mounted) showMessage(context, 'You are on camera. Everyone can see you.');
    } catch (_) {
      _publishing = false;
      if (mounted) showMessage(context, 'Could not start your camera or microphone. Check the permission and try again.');
    }
  }

  Future<void> _stageAction() async {
    if (_stage.onStage) {
      final ok = await guarded(context, () => _repo.leaveStage(widget.roomId));
      if (ok) _poll();
    } else if (_stage.requested) {
      final ok = await guarded(context, () => _repo.leaveStage(widget.roomId));
      if (ok) {
        if (mounted) showMessage(context, 'Request cancelled');
        _poll();
      }
    } else {
      final ok = await guarded(context, () => _repo.requestStage(widget.roomId));
      if (ok) {
        if (mounted) showMessage(context, 'Asked to join. The host will see your request.');
        _poll();
      }
    }
  }

  Future<void> _answerInvite(bool accept) async {
    final ok = await guarded(context, () => _repo.respondStage(widget.roomId, accept: accept));
    if (ok) _poll();
  }

  @override
  void dispose() {
    _live?.dispose();
    _gifts.dispose();
    _media.disconnect();
    super.dispose();
  }

  Future<bool> _sendChat(String text) =>
      guarded(context, () => _repo.chat(widget.roomId, text));

  Future<void> _sendGift() async {
    await showModalBottomSheet<void>(
      context: context,
      useRootNavigator: true,
      showDragHandle: true,
      builder: (c) => Consumer(
        builder: (c, ref, _) {
          final cat = ref.watch(giftCatalogProvider);
          return cat.when(
            loading: () => const SizedBox(height: 200, child: Loader()),
            error: (e, _) => SizedBox(
              height: 200,
              child: ErrorView(
                error: e,
                onRetry: () => ref.invalidate(giftCatalogProvider),
              ),
            ),
            data: (gifts) => ListView(
              shrinkWrap: true,
              children: [
                for (final g in gifts)
                  ListTile(
                    key: Key('live_gift_${g.id}'),
                    leading: const Icon(Icons.card_giftcard),
                    title: Text(g.name),
                    trailing: Text('${g.coins} coins'),
                    onTap: () async {
                      final nav = Navigator.of(c);
                      // One id per tap, reused if the transport retries the call,
                      // so a lost response can never charge twice.
                      final requestId =
                          'live-${DateTime.now().microsecondsSinceEpoch}-${g.id}';
                      final ok = await guarded(c, () async {
                        await _repo.gift(
                          widget.roomId,
                          giftId: g.id,
                          requestId: requestId,
                        );
                      });
                      if (ok) {
                        ref.invalidate(accountProvider);
                        nav.pop();
                        _poll(); // show the announcement right away
                      }
                    },
                  ),
              ],
            ),
          );
        },
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final title = widget.room?.title ?? 'Live';
    Widget body;
    if (_error != null) {
      body = ErrorView(
        error: _error!,
        onRetry: () {
          setState(() => _error = null);
          _connect();
        },
      );
    } else if (!_connected) {
      body = const Loader();
    } else if (_ended) {
      body = const Center(
        child: Text(
          'This stream has ended.',
          key: Key('stream_ended'),
          style: TextStyle(color: Colors.white),
        ),
      );
    } else {
      body = Stack(
        children: [
          Positioned.fill(child: _media.view()),
        Positioned.fill(child: RoomTint(theme: RoomTheme.of(widget.room?.theme ?? 'plain'))),
          const Positioned(left: 0, right: 0, bottom: 0, height: 300, child: IgnorePointer(child: _Scrim(fromBottom: true))),
          const Positioned(left: 0, right: 0, top: 0, height: 120, child: IgnorePointer(child: _Scrim(fromBottom: false))),
          Positioned(
            left: 12,
            right: 12,
            bottom: 70,
            child: GiftOverlay(controller: _gifts),
          ),
          if (_livePoll != null)
            Positioned(
              left: 12,
              right: 12,
              top: 110.0 + (_stage.invited ? 150 : 0),
              child: LivePollCard(
                poll: _livePoll!,
                onVote: (c) async {
                  final p = _livePoll!;
                  final ok = await guarded(context, () => _repo.votePoll(widget.roomId, p.id, c));
                  if (ok) _poll();
                },
              ),
            ),
          if (_cohosts.isNotEmpty)
            Positioned(
              left: 12,
              top: (_pinned.isNotEmpty ? 64.0 : 8.0) + (_stage.invited ? 150 : 0),
              child: Chip(
                key: const Key('cohosts_chip'),
                avatar: const Icon(Icons.groups_2_outlined, size: 18),
                label: Text('With ${_cohosts.join(' and ')}'),
              ),
            ),
          if (_pinned.isNotEmpty)
            Positioned(
              left: 12,
              right: 12,
              top: 8.0 + (_stage.invited ? 150 : 0),
              child: DecoratedBox(
                key: const Key('pinned_banner'),
                decoration: BoxDecoration(
                  color: Colors.black54,
                  borderRadius: BorderRadius.circular(12),
                ),
                child: Padding(
                  padding: const EdgeInsets.all(10),
                  child: Row(
                    children: [
                      const Icon(Icons.push_pin, size: 16, color: Colors.white),
                      const SizedBox(width: 8),
                      Expanded(
                        child: Text(
                          _pinned,
                          style: const TextStyle(color: Colors.white),
                        ),
                      ),
                    ],
                  ),
                ),
              ),
            ),
          if (_stage.invited)
            Positioned(
              left: 12,
              right: 12,
              top: 8,
              child: StageInviteCard(
                onJoin: () => _answerInvite(true),
                onDecline: () => _answerInvite(false),
              ),
            ),
          Positioned(
            left: 12,
            right: 12,
            bottom: 8,
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.end,
              children: [
                Expanded(
                  child: ChatPanel(lines: _chat, onSend: _sendChat),
                ),
                const SizedBox(width: 8),
                if (!_stage.invited) ...[
                  FloatingActionButton.small(
                    key: Key(_stage.onStage ? 'leave_stage' : (_stage.requested ? 'cancel_request' : 'ask_to_join')),
                    heroTag: 'stage_btn',
                    backgroundColor: _stage.onStage ? Palette.danger : Colors.white.withValues(alpha: 0.2),
                    foregroundColor: Colors.white,
                    elevation: 0,
                    onPressed: _stageAction,
                    tooltip: _stage.onStage ? 'Leave the stage' : (_stage.requested ? 'Cancel your request' : 'Ask to join on camera'),
                    child: Icon(_stage.onStage ? Icons.call_end : (_stage.requested ? Icons.hourglass_top : Icons.pan_tool_outlined)),
                  ),
                  const SizedBox(width: 8),
                ],
                FloatingActionButton.small(
                  key: const Key('send_live_gift'),
                  heroTag: 'gift_btn',
                  backgroundColor: ModeLight.daylight.start,
                  foregroundColor: ModeLight.daylight.ink,
                  elevation: 0,
                  onPressed: _sendGift,
                  tooltip: 'Send a gift',
                  child: const Icon(Icons.card_giftcard),
                ),
              ],
            ),
          ),
        ],
      );
    }
    return Scaffold(
      backgroundColor: Colors.black,
      appBar: AppBar(
        title: Text(title, overflow: TextOverflow.ellipsis),
        actions: [if (_error == null && _connected && !_ended) const Padding(padding: EdgeInsets.only(right: 16), child: Center(child: _LivePill()))],
      ),
      body: body,
    );
  }
}

/// A soft dark fade at the top or bottom of the video, so text and buttons on
/// top of it stay readable whatever the picture is.
class _Scrim extends StatelessWidget {
  const _Scrim({required this.fromBottom});
  final bool fromBottom;

  @override
  Widget build(BuildContext context) => DecoratedBox(
        decoration: BoxDecoration(
          gradient: LinearGradient(
            begin: fromBottom ? Alignment.bottomCenter : Alignment.topCenter,
            end: fromBottom ? Alignment.topCenter : Alignment.bottomCenter,
            colors: [Colors.black.withValues(alpha: 0.7), Colors.transparent],
          ),
        ),
      );
}

/// Red "LIVE" badge for the top bar.
class _LivePill extends StatelessWidget {
  const _LivePill();

  @override
  Widget build(BuildContext context) => Container(
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
        decoration: BoxDecoration(color: const Color(0xFFD7263D), borderRadius: BorderRadius.circular(Radii.pill)),
        child: Row(mainAxisSize: MainAxisSize.min, children: const [
          Icon(Icons.circle, size: 8, color: Colors.white),
          SizedBox(width: 6),
          Text('LIVE', style: TextStyle(color: Colors.white, fontWeight: FontWeight.w800, fontSize: 12, letterSpacing: 0.6)),
        ]),
      );
}
