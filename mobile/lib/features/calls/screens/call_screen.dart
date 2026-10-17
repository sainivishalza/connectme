import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/network/api_client.dart';
import '../../../core/theme/tokens.dart';
import '../call_media.dart';
import '../calls_repository.dart';

/// A call in progress (or ringing, for the caller). The media room is joined
/// straight away; the server's call status says whether the other person
/// answered, declined, or never picked up.
class CallScreen extends ConsumerStatefulWidget {
  const CallScreen({super.key, required this.started, this.pollInterval = const Duration(seconds: 2)});
  final StartedCall started;
  final Duration pollInterval;

  @override
  ConsumerState<CallScreen> createState() => _CallScreenState();
}

class _CallScreenState extends ConsumerState<CallScreen> {
  late final CallMedia _media;
  late final CallsRepository _repo;
  Timer? _poll;
  Timer? _clock;
  String _status = 'ringing';
  bool _micOn = true;
  late bool _camOn = widget.started.call.video;
  bool _hungUp = false;
  Object? _error;
  int _seconds = 0;

  CallInfo get call => widget.started.call;

  @override
  void initState() {
    super.initState();
    _repo = ref.read(callsRepositoryProvider);
    _media = ref.read(callMediaFactoryProvider)();
    _status = call.status;
    _join();
    _poll = Timer.periodic(widget.pollInterval, (_) => _check());
  }

  Future<void> _join() async {
    try {
      await _media.connect(widget.started.connect.url, widget.started.connect.token, video: call.video);
    } catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  Future<void> _check() async {
    try {
      final c = await _repo.get(call.id);
      if (!mounted) return;
      if (c.status != _status) setState(() => _status = c.status);
      if (c.status == 'active') _clock ??= Timer.periodic(const Duration(seconds: 1), (_) => setState(() => _seconds++));
      if (c.over) _finish();
    } on ApiException catch (e) {
      if (e.status == 404 && mounted) _finish();
    } catch (_) {
      // transient; retried
    }
  }

  void _finish() {
    if (_hungUp) return;
    _hungUp = true;
    _poll?.cancel();
    _clock?.cancel();
    if (mounted) Navigator.of(context).maybePop();
  }

  Future<void> _hangUp() async {
    _hungUp = true;
    _poll?.cancel();
    _clock?.cancel();
    unawaited(_repo.end(call.id).then((_) {}, onError: (_) {}));
    if (mounted) Navigator.of(context).maybePop();
  }

  @override
  void dispose() {
    _poll?.cancel();
    _clock?.cancel();
    // Leaving the screen any other way (back button) also ends the call.
    if (!_hungUp) unawaited(_repo.end(call.id).then((_) {}, onError: (_) {}));
    _media.disconnect();
    super.dispose();
  }

  String _label() {
    if (_error != null) return 'Could not connect';
    return switch (_status) {
      'ringing' => call.mine ? 'Calling…' : 'Connecting…',
      'active' => '${(_seconds ~/ 60).toString().padLeft(2, '0')}:${(_seconds % 60).toString().padLeft(2, '0')}',
      'declined' => 'Declined',
      'missed' => 'No answer',
      _ => 'Call ended',
    };
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final name = call.peer?.title ?? 'Anonymous';
    return Scaffold(
      backgroundColor: Palette.midnight,
      body: Stack(
        children: [
          if (call.video) Positioned.fill(child: _media.remoteView()),
          if (!call.video || _status != 'active')
            Positioned.fill(
              child: Container(
                decoration: BoxDecoration(gradient: ModeLight.dusk.gradient),
                child: SafeArea(
                  child: Center(
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        const Icon(Icons.person_outline, size: 72, color: Colors.white70),
                        const SizedBox(height: 16),
                        Text(name, style: t.textTheme.headlineMedium?.copyWith(color: Colors.white)),
                      ],
                    ),
                  ),
                ),
              ),
            ),
          SafeArea(
            child: Padding(
              padding: const EdgeInsets.all(20),
              child: Align(
                alignment: Alignment.topCenter,
                child: Text(_label(), key: const Key('call_status'), style: t.textTheme.titleMedium?.copyWith(color: Colors.white)),
              ),
            ),
          ),
          if (call.video && _camOn)
            Positioned(
              right: 16,
              top: 64,
              width: 104,
              height: 148,
              child: ClipRRect(borderRadius: BorderRadius.circular(Radii.card), child: _media.localView()),
            ),
          SafeArea(
            child: Align(
              alignment: Alignment.bottomCenter,
              child: Padding(
                padding: const EdgeInsets.only(bottom: 28),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    _RoundControl(
                      key: const Key('call_mute'),
                      icon: _micOn ? Icons.mic : Icons.mic_off,
                      tooltip: _micOn ? 'Mute' : 'Unmute',
                      onTap: () {
                        setState(() => _micOn = !_micOn);
                        _media.setMic(_micOn);
                      },
                    ),
                    const SizedBox(width: 20),
                    _RoundControl(
                      key: const Key('call_hangup'),
                      icon: Icons.call_end,
                      tooltip: 'End call',
                      background: Palette.danger,
                      size: 68,
                      onTap: _hangUp,
                    ),
                    if (call.video) ...[
                      const SizedBox(width: 20),
                      _RoundControl(
                        key: const Key('call_camera'),
                        icon: _camOn ? Icons.videocam : Icons.videocam_off,
                        tooltip: _camOn ? 'Turn camera off' : 'Turn camera on',
                        onTap: () {
                          setState(() => _camOn = !_camOn);
                          _media.setCamera(_camOn);
                        },
                      ),
                    ],
                  ],
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _RoundControl extends StatelessWidget {
  const _RoundControl({super.key, required this.icon, required this.tooltip, required this.onTap, this.background, this.size = 56});
  final IconData icon;
  final String tooltip;
  final VoidCallback onTap;
  final Color? background;
  final double size;

  @override
  Widget build(BuildContext context) {
    return Tooltip(
      message: tooltip,
      child: Material(
        color: background ?? Colors.white.withValues(alpha: 0.18),
        shape: const CircleBorder(),
        child: InkWell(
          customBorder: const CircleBorder(),
          onTap: onTap,
          child: SizedBox(width: size, height: size, child: Icon(icon, color: Colors.white, size: size * 0.45)),
        ),
      ),
    );
  }
}
