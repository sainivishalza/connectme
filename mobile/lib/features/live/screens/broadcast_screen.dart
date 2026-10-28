import 'dart:async';

import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../shared/widgets/app_snack.dart';
import '../live_repository.dart';
import '../media_session.dart';
import '../widgets/chat_panel.dart';
import '../widgets/gift_overlay.dart';
import '../widgets/live_poll_card.dart';
import '../widgets/stage_widgets.dart';
import '../widgets/room_theme.dart';
import '../../../core/realtime/live_refresh.dart';

/// Go live: choose a title, then publish camera and microphone.
class BroadcastScreen extends ConsumerStatefulWidget {
  const BroadcastScreen({
    super.key,
    this.pollInterval = const Duration(seconds: 2),
  });
  final Duration pollInterval;

  @override
  ConsumerState<BroadcastScreen> createState() => _BroadcastScreenState();
}

class _BroadcastScreenState extends ConsumerState<BroadcastScreen> {
  final _title = TextEditingController();
  late final LiveRepository _repo;
  LiveMedia? _media;
  String? _roomId;
  bool _busy = false;
  LiveRefresh? _live;
  int _lastEvent = 0;
  bool _polling = false;
  final List<LiveEvent> _chat = [];
  final _gifts = GiftOverlayController();
  String _topic = 'chat';
  String _theme = 'plain';
  String _pinned = '';
  LivePoll? _livePoll;
  final _stage = ValueNotifier<StageState>(const StageState());

  @override
  void initState() {
    super.initState();
    _repo = ref.read(liveRepositoryProvider); // dispose() must not touch ref
  }

  @override
  void dispose() {
    _live?.dispose();
    _gifts.dispose();
    _stage.dispose();
    _title.dispose();
    _media?.disconnect();
    // Leaving the screen ends the stream so no room is left running.
    final id = _roomId;
    if (id != null) _repo.end(id).ignore();
    super.dispose();
  }

  Future<void> _start() async {
    final title = _title.text.trim();
    if (title.length < 3) {
      showMessage(context, 'Give your stream a title (3+ characters).');
      return;
    }
    setState(() => _busy = true);
    final media = ref.read(liveMediaFactoryProvider)();
    final ok = await guarded(context, () async {
      final started = await ref
          .read(liveRepositoryProvider)
          .start(title, topic: _topic, theme: _theme);
      _roomId = started.room.id;
      await media.connect(
        started.connect.url,
        started.connect.token,
        publish: true,
      );
    });
    if (!mounted) return;
    setState(() {
      _busy = false;
      if (ok) _media = media;
    });
    if (ok && _roomId != null) {
      _live = LiveRefresh.of(ref, kind: 'live', id: _roomId!, interval: widget.pollInterval, onTick: _poll).start();
    }
    if (!ok) {
      // Don't leave a half-started room behind.
      final id = _roomId;
      _roomId = null;
      if (id != null) ref.read(liveRepositoryProvider).end(id).ignore();
      media.disconnect();
    }
  }

  Future<void> _poll() async {
    final id = _roomId;
    if (_polling || id == null || !mounted) return;
    _polling = true;
    try {
      final page = await _repo.page(id, after: _lastEvent);
      final events = page.events;
      if (!mounted) return;
      if (page.pinned != _pinned) setState(() => _pinned = page.pinned);
      if (page.stage != _stage.value) _stage.value = page.stage;
      final np = page.poll;
      if (np?.id != _livePoll?.id || np?.counts.join(',') != _livePoll?.counts.join(',') || np?.closed != _livePoll?.closed) {
        setState(() => _livePoll = np);
      }
      for (final e in events) {
        if (e.id > _lastEvent) _lastEvent = e.id;
        _chat.add(
          e,
        ); // gifts show as lines too: the host sees who is supporting them
        if (e.isGift) _gifts.show('${e.name} ${e.text}');
      }
      if (events.isNotEmpty) setState(() {});
    } catch (_) {
      // transient; retried on the next tick
    } finally {
      _polling = false;
    }
  }

  Future<void> _moderate(LiveEvent line) async {
    final id = _roomId;
    if (id == null || line.ref.isEmpty) return;
    final action = await showModalBottomSheet<String>(
      context: context,
      useRootNavigator: true,
      showDragHandle: true,
      builder: (c) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            ListTile(
              title: Text(line.name),
              subtitle: Text(
                line.text,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
              ),
            ),
            ListTile(
              key: const Key('mute_viewer'),
              leading: const Icon(Icons.volume_off),
              title: Text(AppLocalizations.of(context)?.sMuteInChat ?? 'Mute in chat'),
              onTap: () => Navigator.pop(c, 'mute'),
            ),
            ListTile(
              key: const Key('make_cohost'),
              leading: const Icon(Icons.group_add_outlined),
              title: const Text('Invite to join on camera'),
              onTap: () => Navigator.pop(c, 'cohost'),
            ),
            ListTile(
              key: const Key('kick_viewer'),
              leading: const Icon(Icons.person_remove),
              title: const Text('Remove from stream'),
              onTap: () => Navigator.pop(c, 'kick'),
            ),
          ],
        ),
      ),
    );
    if (action == null || !mounted) return;
    final ok = await guarded(
      context,
      () => action == 'cohost'
          ? _repo.inviteStage(id, line.ref)
          : _repo.moderate(id, ref: line.ref, action: action),
    );
    if (ok && mounted) {
      showMessage(
        context,
        switch (action) {
          'mute' => '${line.name} is muted',
          'cohost' => 'Invited ${line.name}. They choose whether to join.',
          _ => '${line.name} was removed',
        },
      );
    }
  }

  /// The hands that are up: let each person on camera, or turn them down.
  Future<void> _openRequests() async {
    final id = _roomId;
    if (id == null) return;
    await showModalBottomSheet<void>(
      context: context,
      useRootNavigator: true,
      showDragHandle: true,
      isScrollControlled: true,
      builder: (c) => ValueListenableBuilder<StageState>(
        valueListenable: _stage,
        builder: (c, st, _) => StageRequestsSheet(
          state: st,
          onDecide: _decide,
        ),
      ),
    );
  }

  Future<void> _decide(StageAsk r, bool accept) async {
    final id = _roomId;
    if (id == null) return;
    final ok = await guarded(context, () => _repo.decideStage(id, r.ref, accept: accept));
    if (ok) {
      _stage.value = StageState(
        me: _stage.value.me,
        requests: [for (final x in _stage.value.requests) if (x.ref != r.ref) x],
        guests: _stage.value.guests + (accept ? 1 : 0),
        max: _stage.value.max,
      );
      _poll();
    }
  }

  Future<void> _newPoll() async {
    final id = _roomId;
    if (id == null) return;
    final r = await showDialog<(String, List<String>)>(context: context, builder: (_) => const _PollDialog());
    if (r == null || !mounted) return;
    await guarded(context, () => _repo.startPoll(id, r.$1, r.$2));
  }

  Future<void> _editPin() async {
    final id = _roomId;
    if (id == null) return;
    final text = await showDialog<String>(
      context: context,
      builder: (_) => _PinDialog(initial: _pinned),
    );
    if (text == null || !mounted) return;
    final ok = await guarded(context, () => _repo.pin(id, text));
    if (ok && mounted) setState(() => _pinned = text);
  }

  Future<void> _stop() async {
    _live?.dispose();
    final id = _roomId;
    _roomId = null;
    await _media?.disconnect();
    _media = null;
    if (id != null && mounted) {
      await guarded(context, () => ref.read(liveRepositoryProvider).end(id));
    }
    ref.invalidate(liveRoomsProvider);
    if (!mounted) return;
    Navigator.of(context).maybePop();
  }

  @override
  Widget build(BuildContext context) {
    final media = _media;
    if (media != null) {
      return Scaffold(
        backgroundColor: Colors.black,
        body: Stack(
          children: [
            Positioned.fill(child: media.view()),
          Positioned.fill(child: RoomTint(theme: RoomTheme.of(_theme))),
          if (_livePoll != null)
            Positioned(
              left: 12,
              right: 12,
              top: 150,
              child: LivePollCard(
                poll: _livePoll!,
                isHost: true,
                onClose: () => guarded(context, () => _repo.closePoll(_roomId!)),
              ),
            ),
            SafeArea(
              child: Align(
                alignment: Alignment.topLeft,
                child: Padding(
                  padding: const EdgeInsets.all(12),
                  child: Wrap(
                    spacing: 8,
                    children: [
                      Chip(
                        avatar: const Icon(Icons.sensors, color: Colors.red),
                        label: Text(_title.text.trim()),
                      ),
                      ValueListenableBuilder<StageState>(
                        valueListenable: _stage,
                        builder: (c, st, _) => st.requests.isEmpty && st.guests == 0
                            ? const SizedBox.shrink()
                            : ActionChip(
                                key: const Key('stage_requests'),
                                backgroundColor: st.requests.isEmpty ? null : Theme.of(context).colorScheme.primary,
                                side: BorderSide.none,
                                avatar: Icon(st.requests.isEmpty ? Icons.groups_2_outlined : Icons.pan_tool, size: 18, color: st.requests.isEmpty ? null : Theme.of(context).colorScheme.onPrimary),
                                label: Text(st.requests.isEmpty
                                    ? 'On camera: ${st.guests + 1}'
                                    : 'Asking to join (${st.requests.length})',
                                    style: st.requests.isEmpty ? null : TextStyle(color: Theme.of(context).colorScheme.onPrimary, fontWeight: FontWeight.w700)),
                                onPressed: _openRequests,
                              ),
                      ),
                      ActionChip(
                        key: const Key('new_poll'),
                        avatar: const Icon(Icons.poll_outlined, size: 18),
                        label: Text(AppLocalizations.of(context)?.sPoll ?? 'Poll'),
                        onPressed: _newPoll,
                      ),
                      ActionChip(
                        key: const Key('pin_message'),
                        avatar: const Icon(Icons.push_pin_outlined, size: 18),
                        label: Text(
                          _pinned.isEmpty ? 'Pin a message' : _pinned,
                          overflow: TextOverflow.ellipsis,
                        ),
                        onPressed: _editPin,
                      ),
                    ],
                  ),
                ),
              ),
            ),
            Positioned(
              left: 12,
              right: 12,
              bottom: 200,
              top: 120,
              child: GiftOverlay(controller: _gifts),
            ),
            SafeArea(
              child: Align(
                alignment: Alignment.bottomLeft,
                child: Padding(
                  padding: const EdgeInsets.fromLTRB(12, 0, 12, 76),
                  child: ChatPanel(
                    lines: _chat,
                    onLineTap: _moderate,
                    onSend: (text) =>
                        guarded(context, () => _repo.chat(_roomId!, text)),
                  ),
                ),
              ),
            ),
            SafeArea(
              child: Align(
                alignment: Alignment.bottomCenter,
                child: Padding(
                  padding: const EdgeInsets.all(16),
                  child: FilledButton.icon(
                    key: const Key('end_stream'),
                    style: FilledButton.styleFrom(backgroundColor: Colors.red),
                    onPressed: _stop,
                    icon: const Icon(Icons.stop),
                    label: Text(AppLocalizations.of(context)?.sEndStream ?? 'End stream'),
                  ),
                ),
              ),
            ),
          ],
        ),
      );
    }
    return Scaffold(
      appBar: AppBar(title: Text(AppLocalizations.of(context)?.sGoLive ?? 'Go live')),
      body: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text(
              'Anyone signed in can watch, so only share what you are happy for the public to see. '
              'You can end the stream at any time.',
            ),
            const SizedBox(height: 16),
            Wrap(key: const Key('stream_theme'), spacing: 8, runSpacing: 4, children: [
              for (final th in RoomTheme.all)
                ChoiceChip(
                  key: Key('theme_${th.id}'),
                  label: Text(th.label),
                  selected: _theme == th.id,
                  onSelected: (_) => setState(() => _theme = th.id),
                ),
            ]),
            const SizedBox(height: 12),
            DropdownButtonFormField<String>(
              key: const Key('stream_topic'),
              initialValue: _topic,
              decoration: const InputDecoration(
                labelText: 'Topic',
                border: OutlineInputBorder(),
              ),
              items: [
                for (final t in liveTopics)
                  DropdownMenuItem(value: t, child: Text(topicLabel(t))),
              ],
              onChanged: (v) => setState(() => _topic = v ?? 'chat'),
            ),
            const SizedBox(height: 12),
            TextField(
              key: const Key('stream_title'),
              controller: _title,
              maxLength: 80,
              decoration: const InputDecoration(
                labelText: 'Title',
                border: OutlineInputBorder(),
              ),
            ),
            const SizedBox(height: 8),
            FilledButton.icon(
              key: const Key('start_stream'),
              onPressed: _busy ? null : _start,
              icon: _busy
                  ? const SizedBox(
                      width: 18,
                      height: 18,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Icon(Icons.videocam),
              label: Text(AppLocalizations.of(context)?.sStartStreaming ?? 'Start streaming'),
            ),
          ],
        ),
      ),
    );
  }
}

class _PinDialog extends StatefulWidget {
  const _PinDialog({required this.initial});
  final String initial;

  @override
  State<_PinDialog> createState() => _PinDialogState();
}

class _PinDialogState extends State<_PinDialog> {
  late final _ctl = TextEditingController(text: widget.initial);

  @override
  void dispose() {
    _ctl.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text(AppLocalizations.of(context)?.sPinnedMessage ?? 'Pinned message'),
    content: TextField(
      key: const Key('pin_text'),
      controller: _ctl,
      maxLength: 120,
      decoration: const InputDecoration(hintText: 'Shown to everyone watching'),
    ),
    actions: [
      TextButton(
        key: const Key('unpin'),
        onPressed: () => Navigator.pop(context, ''),
        child: Text(AppLocalizations.of(context)?.sClear ?? 'Clear'),
      ),
      FilledButton(
        key: const Key('save_pin'),
        onPressed: () => Navigator.pop(context, _ctl.text.trim()),
        child: Text(AppLocalizations.of(context)?.sPin ?? 'Pin'),
      ),
    ],
  );
}

class _PollDialog extends StatefulWidget {
  const _PollDialog();

  @override
  State<_PollDialog> createState() => _PollDialogState();
}

class _PollDialogState extends State<_PollDialog> {
  final _q = TextEditingController();
  final _opts = [TextEditingController(), TextEditingController(), TextEditingController(), TextEditingController()];

  @override
  void dispose() {
    _q.dispose();
    for (final c in _opts) {
      c.dispose();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
        title: Text(AppLocalizations.of(context)?.sAskTheRoom ?? 'Ask the room'),
        content: SingleChildScrollView(
          child: Column(mainAxisSize: MainAxisSize.min, children: [
            TextField(key: const Key('poll_question'), controller: _q, maxLength: 80, decoration: const InputDecoration(labelText: 'Question')),
            for (var i = 0; i < 4; i++)
              TextField(key: Key('poll_answer_$i'), controller: _opts[i], maxLength: 30, decoration: InputDecoration(labelText: i < 2 ? 'Answer ${i + 1}' : 'Answer ${i + 1} (optional)', counterText: '')),
          ]),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context), child: Text(AppLocalizations.of(context)?.sCancel ?? 'Cancel')),
          FilledButton(
            key: const Key('poll_start'),
            onPressed: () {
              final answers = _opts.map((c) => c.text.trim()).where((s) => s.isNotEmpty).toList();
              if (_q.text.trim().isEmpty || answers.length < 2) return;
              Navigator.pop(context, (_q.text.trim(), answers));
            },
            child: Text(AppLocalizations.of(context)?.sStart ?? 'Start'),
          ),
        ],
      );
}
