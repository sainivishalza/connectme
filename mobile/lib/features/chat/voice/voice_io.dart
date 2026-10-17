import 'dart:async';

import 'package:audioplayers/audioplayers.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:image_picker/image_picker.dart' show XFile;
import 'package:path_provider/path_provider.dart';
import 'package:record/record.dart';

/// A finished recording, ready to be encrypted and sent.
class VoiceClip {
  VoiceClip({required this.bytes, required this.mime, required this.seconds});
  final Uint8List bytes;
  final String mime;
  final int seconds;
}

/// Records one voice note at a time. Behind an interface so screens and tests
/// never touch the microphone.
abstract class VoiceRecorder {
  /// Starts recording. Returns false when the microphone is not allowed.
  Future<bool> start();

  /// Stops and returns the clip (null if nothing usable was recorded).
  Future<VoiceClip?> stop();

  /// Stops and throws the recording away.
  Future<void> cancel();

  Future<void> dispose();
}

/// Plays one voice note at a time from decrypted bytes.
abstract class VoicePlayer {
  Future<void> play(Uint8List bytes, String mime);
  Future<void> pause();
  Future<void> resume();
  Stream<Duration> get position;
  Stream<void> get completed;
  Future<void> dispose();
}

/// Longest voice note, in seconds. The server accepts far more, but a short
/// limit keeps messages quick to send, play and (for burn timers) discard.
const maxVoiceSeconds = 120;

class DeviceVoiceRecorder implements VoiceRecorder {
  final AudioRecorder _rec = AudioRecorder();
  DateTime? _startedAt;
  String _mime = 'audio/mp4';

  static const _encoders = [
    (AudioEncoder.aacLc, 'audio/mp4', 'm4a'),
    (AudioEncoder.opus, 'audio/webm', 'webm'),
  ];

  @override
  Future<bool> start() async {
    if (!await _rec.hasPermission()) return false;
    // Use the first format this platform can record (Safari: AAC; Chrome: either).
    for (final (enc, mime, ext) in _encoders) {
      if (!await _rec.isEncoderSupported(enc)) continue;
      _mime = mime;
      final path = kIsWeb
          ? ''
          : '${(await getTemporaryDirectory()).path}/voice-${DateTime.now().microsecondsSinceEpoch}.$ext';
      await _rec.start(
        RecordConfig(encoder: enc, numChannels: 1, bitRate: 48000),
        path: path,
      );
      _startedAt = DateTime.now();
      return true;
    }
    return false;
  }

  @override
  Future<VoiceClip?> stop() async {
    final path = await _rec.stop();
    final started = _startedAt;
    _startedAt = null;
    if (path == null || started == null) return null;
    final bytes = await XFile(path).readAsBytes();
    final seconds = DateTime.now()
        .difference(started)
        .inSeconds
        .clamp(1, maxVoiceSeconds);
    if (bytes.isEmpty) return null;
    return VoiceClip(bytes: bytes, mime: _mime, seconds: seconds);
  }

  @override
  Future<void> cancel() async {
    _startedAt = null;
    await _rec.cancel();
  }

  @override
  Future<void> dispose() => _rec.dispose();
}

class DeviceVoicePlayer implements VoicePlayer {
  final AudioPlayer _p = AudioPlayer();

  @override
  Future<void> play(Uint8List bytes, String mime) =>
      _p.play(BytesSource(bytes, mimeType: mime));
  @override
  Future<void> pause() => _p.pause();
  @override
  Future<void> resume() => _p.resume();
  @override
  Stream<Duration> get position => _p.onPositionChanged;
  @override
  Stream<void> get completed => _p.onPlayerComplete;
  @override
  Future<void> dispose() => _p.dispose();
}

/// Factories, so each bubble/composer owns its own instance and tests can
/// substitute fakes.
final voiceRecorderFactoryProvider = Provider<VoiceRecorder Function()>(
  (ref) => DeviceVoiceRecorder.new,
);
final voicePlayerFactoryProvider = Provider<VoicePlayer Function()>(
  (ref) => DeviceVoicePlayer.new,
);
