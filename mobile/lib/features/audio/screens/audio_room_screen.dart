import 'dart:async';

import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/network/api_client.dart';
import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../../calls/call_media.dart';
import '../audio_repository.dart';

/// Inside an audio room: the stage, the audience and the controls that match
/// your role. The server decides who may speak; this screen only reflects it.
class AudioRoomScreen extends ConsumerStatefulWidget {
  const AudioRoomScreen({super.key, required this.roomId, this.started, this.pollInterval = const Duration(seconds: 4)});
  final String roomId;
  final StartedAudio? started; // present when I just opened the room
  final Duration pollInterval;

  @override
  ConsumerState<AudioRoomScreen> createState() => _AudioRoomScreenState();
}

class _AudioRoomScreenState extends ConsumerState<AudioRoomScreen> {
  late final AudioRepository _repo;
  late final CallMedia Function() _newMedia;
  CallMedia? _media;
  AudioState? _state;
  Object? _error;
  Timer? _timer;
  bool _ended = false;
  bool _micOn = false;
  bool _handUp = false;
  bool _polling = false;
  String _role = 'listener';

  @override
  void initState() {
    super.initState();
    _repo = ref.read(audioRepositoryProvider);
    _newMedia = ref.read(callMediaFactoryProvider);
    _enter();
  }

  Future<void> _connect(AudioConnect c) async {
    final old = _media;
    _media = null;
    await old?.disconnect();
    final media = _newMedia();
    await media.connect(c.url, c.token, video: false);
    await media.setMic(c.canPublish);
    _media = media;
    _role = c.role;
    if (mounted) setState(() => _micOn = c.canPublish);
  }

  Future<void> _enter() async {
    try {
      final c = widget.started?.connect ?? await _repo.join(widget.roomId);
      await _connect(c);
      await _poll();
      _timer = Timer.periodic(widget.pollInterval, (_) => _poll());
    } catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  Future<void> _poll() async {
    if (_polling || _ended || !mounted) return;
    _polling = true;
    try {
      final s = await _repo.state(widget.roomId);
      if (!mounted) return;
      final changed = s.myRole != _role && _media != null;
      setState(() {
        _state = s;
        _error = null;
        if (s.myRole == 'listener' && !_handUpStillValid(s)) _handUp = false;
      });
      if (changed) {
        // Promoted or demoted: take a token that carries the new permission.
        await _connect(await _repo.join(widget.roomId));
        if (mounted && _role == 'speaker') showMessage(context, 'You are on stage. Unmute to speak.');
      }
    } on ApiException catch (e) {
      if (e.status == 410 || e.status == 404) {
        _timer?.cancel();
        if (mounted) setState(() => _ended = true);
        await _media?.disconnect();
        _media = null;
      }
    } catch (_) {
      // a missed poll is retried on the next tick
    } finally {
      _polling = false;
    }
  }

  bool _handUpStillValid(AudioState s) => s.members.any((m) => m.me && m.handRaised);

  @override
  void dispose() {
    _timer?.cancel();
    final m = _media;
    if (m != null) m.disconnect();
    super.dispose();
  }

  Future<void> _leave() async {
    _timer?.cancel();
    await _media?.disconnect();
    _media = null;
    try {
      await _repo.leave(widget.roomId);
    } catch (_) {}
    if (mounted) Navigator.of(context).maybePop();
  }

  Future<void> _end() async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (c) => AlertDialog(
        title: const Text('End this room for everyone?'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(c, false), child: const Text('Keep it open')),
          FilledButton(key: const Key('confirm_end_audio'), onPressed: () => Navigator.pop(c, true), child: Text(AppLocalizations.of(context)?.sEndRoom ?? 'End room')),
        ],
      ),
    );
    if (ok != true || !mounted) return;
    if (await guarded(context, () => _repo.end(widget.roomId)) && mounted) {
      _timer?.cancel();
      await _media?.disconnect();
      _media = null;
      if (mounted) Navigator.of(context).maybePop();
    }
  }

  Future<void> _toggleMic() async {
    final next = !_micOn;
    await _media?.setMic(next);
    if (mounted) setState(() => _micOn = next);
  }

  Future<void> _toggleHand() async {
    final next = !_handUp;
    if (await guarded(context, () => _repo.hand(widget.roomId, up: next))) {
      if (mounted) setState(() => _handUp = next);
      _poll();
    }
  }

  Future<void> _manage(AudioMember m) async {
    final action = await showModalBottomSheet<String>(
      context: context, useRootNavigator: true,
      builder: (c) => SafeArea(
        child: Column(mainAxisSize: MainAxisSize.min, children: [
          ListTile(title: Text(m.label, style: Theme.of(c).textTheme.titleMedium)),
          if (!m.onStage)
            ListTile(key: const Key('promote'), leading: const Icon(Icons.mic_none), title: const Text('Invite to speak'), onTap: () => Navigator.pop(c, 'promote')),
          if (m.role == 'speaker')
            ListTile(key: const Key('demote'), leading: const Icon(Icons.mic_off_outlined), title: const Text('Move to audience'), onTap: () => Navigator.pop(c, 'demote')),
          ListTile(key: const Key('kick'), leading: const Icon(Icons.person_remove_outlined), title: const Text('Remove from room'), onTap: () => Navigator.pop(c, 'kick')),
        ]),
      ),
    );
    if (action == null || !mounted) return;
    if (await guarded(context, () => _repo.setRole(widget.roomId, m.id, action))) _poll();
  }

  Widget _person(AudioMember m, {required bool host}) {
    final t = Theme.of(context);
    final speaking = m.onStage;
    return InkWell(
      key: Key('member_${m.id}'),
      borderRadius: BorderRadius.circular(16),
      onTap: host && !m.me && m.role != 'host' ? () => _manage(m) : null,
      child: SizedBox(
        width: 84,
        child: Column(mainAxisSize: MainAxisSize.min, children: [
          Stack(clipBehavior: Clip.none, children: [
            CircleAvatar(
              radius: speaking ? 30 : 24,
              backgroundColor: speaking ? t.colorScheme.primaryContainer : t.colorScheme.surfaceContainerHighest,
              child: Text(m.label.isEmpty ? '?' : m.label.characters.first.toUpperCase(), style: t.textTheme.titleMedium),
            ),
            if (m.handRaised) const Positioned(right: -4, top: -4, child: Text('✋', key: Key('hand_up'))),
          ]),
          const SizedBox(height: 6),
          Text(m.me ? '${m.label} (you)' : m.label, maxLines: 2, textAlign: TextAlign.center, overflow: TextOverflow.ellipsis, style: t.textTheme.bodySmall),
        ]),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final s = _state;
    final isHost = s?.myRole == 'host';
    final onStage = s != null && s.myRole != 'listener';
    Widget body;
    if (_ended) {
      body = EmptyState(
        icon: Icons.mic_off_outlined,
        title: 'This room has ended',
        message: 'Thanks for listening.',
        action: FilledButton(onPressed: () => Navigator.of(context).maybePop(), child: Text(AppLocalizations.of(context)?.sBackToRooms ?? 'Back to rooms')),
      );
    } else if (s == null) {
      body = _error != null ? ErrorView(error: _error!, onRetry: () => _enter()) : const Loader();
    } else {
      body = ListView(padding: const EdgeInsets.fromLTRB(16, 8, 16, 120), children: [
        Text('On stage', style: t.textTheme.titleMedium),
        const SizedBox(height: 12),
        Wrap(spacing: 8, runSpacing: 16, children: [for (final m in s.stage) _person(m, host: isHost)]),
        const SizedBox(height: 28),
        Text('Listening (${s.audience.length})', style: t.textTheme.titleMedium),
        const SizedBox(height: 12),
        if (s.audience.isEmpty)
          Text('Nobody here yet.', style: t.textTheme.bodySmall)
        else
          Wrap(spacing: 8, runSpacing: 16, children: [for (final m in s.audience) _person(m, host: isHost)]),
      ]);
    }
    return Scaffold(
      appBar: AppBar(
        title: Text(s?.room.title ?? widget.started?.room.title ?? 'Audio room', overflow: TextOverflow.ellipsis),
        actions: [
          if (isHost) IconButton(key: const Key('end_audio'), tooltip: 'End room', icon: const Icon(Icons.stop_circle_outlined), onPressed: _end),
        ],
      ),
      body: body,
      bottomNavigationBar: _ended || s == null
          ? null
          : SafeArea(
              child: Padding(
                padding: const EdgeInsets.fromLTRB(16, 8, 16, 12),
                child: Row(children: [
                  OutlinedButton.icon(key: const Key('leave_audio'), onPressed: _leave, icon: const Icon(Icons.logout), label: Text(isHost ? 'Leave' : 'Leave quietly')),
                  const Spacer(),
                  if (onStage)
                    FilledButton.icon(
                      key: const Key('mic_toggle'),
                      onPressed: _toggleMic,
                      icon: Icon(_micOn ? Icons.mic : Icons.mic_off),
                      label: Text(_micOn ? 'Mute' : 'Unmute'),
                    )
                  else
                    FilledButton.tonalIcon(
                      key: const Key('raise_hand'),
                      onPressed: _toggleHand,
                      icon: const Text('✋'),
                      label: Text(_handUp ? 'Lower hand' : 'Raise hand'),
                    ),
                ]),
              ),
            ),
    );
  }
}
