import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/network/api_client.dart';
import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../../payments/payments_repository.dart';
import '../live_repository.dart';
import '../media_session.dart';
import '../widgets/chat_panel.dart';
import '../widgets/gift_overlay.dart';

/// Watch a stream: subscribe-only video, public chat and gifts.
class LiveRoomScreen extends ConsumerStatefulWidget {
  const LiveRoomScreen({super.key, required this.roomId, this.room, this.pollInterval = const Duration(seconds: 2)});
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
  Timer? _timer;
  int _lastEvent = 0;
  bool _polling = false;
  Object? _error;
  bool _connected = false;
  bool _ended = false;

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
      await _media.connect(info.url, info.token, publish: false);
      if (!mounted) return;
      setState(() => _connected = true);
      _timer = Timer.periodic(widget.pollInterval, (_) => _poll());
      _poll();
    } catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  Future<void> _poll() async {
    if (_polling || !mounted) return;
    _polling = true;
    try {
      final events = await _repo.events(widget.roomId, after: _lastEvent);
      if (!mounted) return;
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
        _timer?.cancel();
        setState(() => _ended = true);
      }
    } catch (_) {
      // transient; retried on the next tick
    } finally {
      _polling = false;
    }
  }

  @override
  void dispose() {
    _timer?.cancel();
    _gifts.dispose();
    _media.disconnect();
    super.dispose();
  }

  Future<bool> _sendChat(String text) => guarded(context, () => _repo.chat(widget.roomId, text));

  Future<void> _sendGift() async {
    await showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      builder: (c) => Consumer(builder: (c, ref, _) {
        final cat = ref.watch(giftCatalogProvider);
        return cat.when(
          loading: () => const SizedBox(height: 200, child: Loader()),
          error: (e, _) => SizedBox(height: 200, child: ErrorView(error: e, onRetry: () => ref.invalidate(giftCatalogProvider))),
          data: (gifts) => ListView(shrinkWrap: true, children: [
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
                  final requestId = 'live-${DateTime.now().microsecondsSinceEpoch}-${g.id}';
                  final ok = await guarded(c, () async {
                    await _repo.gift(widget.roomId, giftId: g.id, requestId: requestId);
                  });
                  if (ok) {
                    ref.invalidate(accountProvider);
                    nav.pop();
                    _poll(); // show the announcement right away
                  }
                },
              ),
          ]),
        );
      }),
    );
  }

  @override
  Widget build(BuildContext context) {
    final title = widget.room?.title ?? 'Live';
    Widget body;
    if (_error != null) {
      body = ErrorView(error: _error!, onRetry: () {
        setState(() => _error = null);
        _connect();
      });
    } else if (!_connected) {
      body = const Loader();
    } else if (_ended) {
      body = const Center(
        child: Text('This stream has ended.', key: Key('stream_ended'), style: TextStyle(color: Colors.white)),
      );
    } else {
      body = Stack(children: [
        Positioned.fill(child: _media.view()),
        Positioned(left: 12, right: 12, bottom: 70, child: GiftOverlay(controller: _gifts)),
        Positioned(
          left: 12,
          right: 12,
          bottom: 8,
          child: Row(crossAxisAlignment: CrossAxisAlignment.end, children: [
            Expanded(child: ChatPanel(lines: _chat, onSend: _sendChat)),
            const SizedBox(width: 8),
            FloatingActionButton.small(
              key: const Key('send_live_gift'),
              onPressed: _sendGift,
              tooltip: 'Send a gift',
              child: const Icon(Icons.card_giftcard),
            ),
          ]),
        ),
      ]);
    }
    return Scaffold(
      backgroundColor: Colors.black,
      appBar: AppBar(title: Text(title, overflow: TextOverflow.ellipsis)),
      body: body,
    );
  }
}
