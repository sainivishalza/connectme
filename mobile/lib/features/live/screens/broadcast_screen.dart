import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../shared/widgets/app_snack.dart';
import '../live_repository.dart';
import '../media_session.dart';
import '../widgets/chat_panel.dart';

/// Go live: choose a title, then publish camera and microphone.
class BroadcastScreen extends ConsumerStatefulWidget {
  const BroadcastScreen({super.key, this.pollInterval = const Duration(seconds: 2)});
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
  Timer? _timer;
  int _lastEvent = 0;
  bool _polling = false;
  final List<LiveEvent> _chat = [];

  @override
  void initState() {
    super.initState();
    _repo = ref.read(liveRepositoryProvider); // dispose() must not touch ref
  }

  @override
  void dispose() {
    _timer?.cancel();
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
      final started = await ref.read(liveRepositoryProvider).start(title);
      _roomId = started.room.id;
      await media.connect(started.connect.url, started.connect.token, publish: true);
    });
    if (!mounted) return;
    setState(() {
      _busy = false;
      if (ok) _media = media;
    });
    if (ok) _timer = Timer.periodic(widget.pollInterval, (_) => _poll());
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
      final events = await _repo.events(id, after: _lastEvent);
      if (!mounted) return;
      for (final e in events) {
        if (e.id > _lastEvent) _lastEvent = e.id;
        _chat.add(e); // gifts show as lines too: the host sees who is supporting them
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
      showDragHandle: true,
      builder: (c) => SafeArea(
        child: Column(mainAxisSize: MainAxisSize.min, children: [
          ListTile(title: Text(line.name), subtitle: Text(line.text, maxLines: 2, overflow: TextOverflow.ellipsis)),
          ListTile(
            key: const Key('mute_viewer'),
            leading: const Icon(Icons.volume_off),
            title: const Text('Mute in chat'),
            onTap: () => Navigator.pop(c, 'mute'),
          ),
          ListTile(
            key: const Key('kick_viewer'),
            leading: const Icon(Icons.person_remove),
            title: const Text('Remove from stream'),
            onTap: () => Navigator.pop(c, 'kick'),
          ),
        ]),
      ),
    );
    if (action == null || !mounted) return;
    final ok = await guarded(context, () => _repo.moderate(id, ref: line.ref, action: action));
    if (ok && mounted) showMessage(context, action == 'mute' ? '${line.name} is muted' : '${line.name} was removed');
  }

  Future<void> _stop() async {
    _timer?.cancel();
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
        body: Stack(children: [
          Positioned.fill(child: media.view()),
          SafeArea(
            child: Align(
              alignment: Alignment.topLeft,
              child: Padding(
                padding: const EdgeInsets.all(12),
                child: Chip(avatar: const Icon(Icons.sensors, color: Colors.red), label: Text(_title.text.trim())),
              ),
            ),
          ),
          SafeArea(
            child: Align(
              alignment: Alignment.bottomLeft,
              child: Padding(
                padding: const EdgeInsets.fromLTRB(12, 0, 12, 76),
                child: ChatPanel(
                  lines: _chat,
                  onLineTap: _moderate,
                  onSend: (text) => guarded(context, () => _repo.chat(_roomId!, text)),
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
                  label: const Text('End stream'),
                ),
              ),
            ),
          ),
        ]),
      );
    }
    return Scaffold(
      appBar: AppBar(title: const Text('Go live')),
      body: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          const Text('Anyone signed in can watch, so only share what you are happy for the public to see. '
              'You can end the stream at any time.'),
          const SizedBox(height: 16),
          TextField(
            key: const Key('stream_title'),
            controller: _title,
            maxLength: 80,
            decoration: const InputDecoration(labelText: 'Title', border: OutlineInputBorder()),
          ),
          const SizedBox(height: 8),
          FilledButton.icon(
            key: const Key('start_stream'),
            onPressed: _busy ? null : _start,
            icon: _busy
                ? const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2))
                : const Icon(Icons.videocam),
            label: const Text('Start streaming'),
          ),
        ]),
      ),
    );
  }
}
