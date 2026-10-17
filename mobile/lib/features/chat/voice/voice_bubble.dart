import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../data/chat_models.dart';
import 'voice_io.dart';

/// Play/pause and progress for one voice note inside a chat bubble. The audio
/// is downloaded and decrypted only when it is first played.
class VoiceBubbleBody extends StatelessWidget {
  const VoiceBubbleBody({super.key, required this.message, required this.load, required this.color});
  final ChatMessage message;
  final Future<Uint8List> Function(ChatMessage)? load;
  final Color color;

  @override
  Widget build(BuildContext context) {
    final l = load;
    return VoicePlayback(
      seconds: int.tryParse(message.text) ?? 0,
      mime: message.mime ?? 'audio/mp4',
      color: color,
      load: l == null ? null : () => l(message),
    );
  }
}

/// A play button, progress bar and clock for any short voice clip: chat voice
/// notes and profile voice intros. [load] runs on the first play only.
class VoicePlayback extends ConsumerStatefulWidget {
  const VoicePlayback({super.key, required this.seconds, required this.mime, required this.load, required this.color});
  final int seconds;
  final String mime;
  final Future<Uint8List> Function()? load;
  final Color color;

  @override
  ConsumerState<VoicePlayback> createState() => _VoicePlaybackState();
}

enum _Phase { idle, loading, playing, paused, failed }

class _VoicePlaybackState extends ConsumerState<VoicePlayback> {
  VoicePlayer? _player;
  StreamSubscription<Duration>? _pos;
  StreamSubscription<void>? _done;
  _Phase _phase = _Phase.idle;
  Duration _at = Duration.zero;

  @override
  void dispose() {
    _pos?.cancel();
    _done?.cancel();
    _player?.dispose();
    super.dispose();
  }

  Future<void> _toggle() async {
    switch (_phase) {
      case _Phase.loading:
        return;
      case _Phase.playing:
        await _player?.pause();
        setState(() => _phase = _Phase.paused);
      case _Phase.paused:
        await _player?.resume();
        setState(() => _phase = _Phase.playing);
      case _Phase.idle || _Phase.failed:
        await _start();
    }
  }

  Future<void> _start() async {
    final load = widget.load;
    if (load == null) return;
    setState(() => _phase = _Phase.loading);
    try {
      final bytes = await load();
      if (!mounted) return;
      final p = _player ??= ref.read(voicePlayerFactoryProvider)();
      _pos ??= p.position.listen((d) {
        if (mounted) setState(() => _at = d);
      });
      _done ??= p.completed.listen((_) {
        if (mounted) {
          setState(() {
            _phase = _Phase.idle;
            _at = Duration.zero;
          });
        }
      });
      await p.play(bytes, widget.mime);
      if (mounted) setState(() => _phase = _Phase.playing);
    } catch (_) {
      if (mounted) setState(() => _phase = _Phase.failed);
    }
  }

  String _fmt(int s) => '${s ~/ 60}:${(s % 60).toString().padLeft(2, '0')}';

  @override
  Widget build(BuildContext context) {
    final fg = widget.color;
    final total = widget.seconds;
    final progress = total > 0 ? (_at.inMilliseconds / (total * 1000)).clamp(0.0, 1.0) : 0.0;
    final playing = _phase == _Phase.playing;
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        IconButton(
          key: const Key('voice_play'),
          tooltip: playing ? 'Pause' : 'Play voice note',
          onPressed: _toggle,
          icon: _phase == _Phase.loading
              ? SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2, color: fg))
              : Icon(playing ? Icons.pause_circle_filled : Icons.play_circle_fill, size: 34, color: fg),
          padding: EdgeInsets.zero,
          constraints: const BoxConstraints(minWidth: 40, minHeight: 40),
        ),
        const SizedBox(width: 6),
        SizedBox(
          width: 110,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisSize: MainAxisSize.min,
            children: [
              ClipRRect(
                borderRadius: BorderRadius.circular(4),
                child: LinearProgressIndicator(value: progress, minHeight: 5, color: fg, backgroundColor: fg.withValues(alpha: 0.25)),
              ),
              const SizedBox(height: 4),
              Text(
                _phase == _Phase.failed ? 'Could not play' : (playing || _phase == _Phase.paused ? _fmt(_at.inSeconds) : _fmt(total)),
                style: TextStyle(fontSize: 12, color: fg.withValues(alpha: 0.85)),
              ),
            ],
          ),
        ),
      ],
    );
  }
}
