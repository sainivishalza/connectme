import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../shared/widgets/app_snack.dart';
import 'voice_io.dart';

/// Replaces the message box while a voice note is being recorded: a running
/// clock, a delete button and a send button. Recording stops by itself at
/// [maxVoiceSeconds] and sends what was recorded.
class VoiceComposer extends ConsumerStatefulWidget {
  const VoiceComposer({super.key, required this.onSend, required this.onClose, this.maxSeconds = maxVoiceSeconds});

  /// Called with the finished clip. The composer closes itself afterwards.
  final Future<void> Function(VoiceClip clip) onSend;
  final VoidCallback onClose;

  /// Longest recording; it stops and sends by itself at this length.
  final int maxSeconds;

  @override
  ConsumerState<VoiceComposer> createState() => _VoiceComposerState();
}

class _VoiceComposerState extends ConsumerState<VoiceComposer> {
  late final VoiceRecorder _rec;
  Timer? _tick;
  int _seconds = 0;
  bool _ready = false;
  bool _finishing = false;

  @override
  void initState() {
    super.initState();
    _rec = ref.read(voiceRecorderFactoryProvider)();
    _begin();
  }

  Future<void> _begin() async {
    bool ok = false;
    try {
      ok = await _rec.start();
    } catch (_) {
      ok = false;
    }
    if (!mounted) return;
    if (!ok) {
      showMessage(context, 'Allow the microphone to send voice notes.');
      widget.onClose();
      return;
    }
    setState(() => _ready = true);
    _tick = Timer.periodic(const Duration(seconds: 1), (_) {
      if (!mounted) return;
      setState(() => _seconds++);
      if (_seconds >= widget.maxSeconds) {
        showMessage(context, 'Recordings are limited to ${widget.maxSeconds >= 60 ? '${widget.maxSeconds ~/ 60} minutes' : '${widget.maxSeconds} seconds'}.');
        _finish(send: true);
      }
    });
  }

  Future<void> _finish({required bool send}) async {
    if (_finishing) return;
    _finishing = true;
    _tick?.cancel();
    try {
      if (!send) {
        await _rec.cancel();
      } else {
        final clip = await _rec.stop();
        if (clip != null && clip.seconds >= 1) {
          await widget.onSend(clip);
        }
      }
    } catch (e) {
      if (mounted) showError(context, e);
    }
    if (mounted) widget.onClose();
  }

  @override
  void dispose() {
    _tick?.cancel();
    if (!_finishing) _rec.cancel().ignore();
    _rec.dispose().ignore();
    super.dispose();
  }

  String _clock(int s) => '${s ~/ 60}:${(s % 60).toString().padLeft(2, '0')}';

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return Row(
      key: const Key('voice_composer'),
      children: [
        IconButton(
          key: const Key('voice_cancel'),
          tooltip: 'Delete recording',
          onPressed: _finishing ? null : () => _finish(send: false),
          icon: const Icon(Icons.delete_outline),
        ),
        Expanded(
          child: Row(
            children: [
              Container(
                width: 10,
                height: 10,
                decoration: BoxDecoration(
                  color: t.colorScheme.error,
                  shape: BoxShape.circle,
                ),
              ),
              const SizedBox(width: 10),
              Text(
                _ready ? 'Recording ${_clock(_seconds)}' : 'Starting…',
                style: t.textTheme.titleSmall,
              ),
            ],
          ),
        ),
        IconButton.filled(
          key: const Key('voice_send'),
          tooltip: 'Send voice note',
          onPressed: _ready && !_finishing ? () => _finish(send: true) : null,
          icon: const Icon(Icons.send),
        ),
      ],
    );
  }
}
