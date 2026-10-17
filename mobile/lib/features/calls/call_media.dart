import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:livekit_client/livekit_client.dart' as lk;

/// The only place calls touch the LiveKit SDK, so screens and tests depend on
/// this small interface and never on WebRTC.
abstract class CallMedia {
  /// Joins the room with a server-issued token and turns on the microphone
  /// (and the camera for a video call).
  Future<void> connect(String url, String token, {required bool video});

  /// True once the other person is in the room.
  ValueListenable<bool> get remoteJoined;

  Widget remoteView();
  Widget localView();

  Future<void> setMic(bool on);
  Future<void> setCamera(bool on);
  Future<void> disconnect();
}

class LiveKitCallMedia implements CallMedia {
  final lk.Room _room = lk.Room();
  final ValueNotifier<bool> _remote = ValueNotifier(false);

  @override
  ValueListenable<bool> get remoteJoined => _remote;

  @override
  Future<void> connect(String url, String token, {required bool video}) async {
    _room.addListener(() => _remote.value = _room.remoteParticipants.isNotEmpty);
    await _room.connect(url, token);
    try {
      await _room.startAudio(); // browsers play remote audio only after a user gesture
    } catch (_) {}
    try {
      await _room.localParticipant?.setMicrophoneEnabled(true);
    } catch (_) {} // listeners may not publish
    if (video) await _room.localParticipant?.setCameraEnabled(true);
    _remote.value = _room.remoteParticipants.isNotEmpty;
  }

  lk.VideoTrack? _first(Iterable<lk.TrackPublication> pubs) {
    for (final p in pubs) {
      final t = p.track;
      if (t is lk.VideoTrack && !p.muted) return t;
    }
    return null;
  }

  Widget _view(lk.VideoTrack? Function() pick, {required bool mirror}) => ListenableBuilder(
        listenable: _room,
        builder: (context, _) {
          final t = pick();
          if (t == null) return const ColoredBox(color: Colors.black);
          return lk.VideoTrackRenderer(t, fit: lk.VideoViewFit.cover, mirrorMode: mirror ? lk.VideoViewMirrorMode.mirror : lk.VideoViewMirrorMode.off);
        },
      );

  @override
  Widget remoteView() =>
      _view(() => _first(_room.remoteParticipants.values.expand((p) => p.videoTrackPublications)), mirror: false);

  @override
  Widget localView() =>
      _view(() => _first(_room.localParticipant?.videoTrackPublications ?? const []), mirror: true);

  @override
  Future<void> setMic(bool on) async {
    await _room.localParticipant?.setMicrophoneEnabled(on);
  }

  @override
  Future<void> setCamera(bool on) async {
    await _room.localParticipant?.setCameraEnabled(on);
  }

  @override
  Future<void> disconnect() async {
    await _room.disconnect();
    await _room.dispose();
    _remote.dispose();
  }
}

/// Creates a fresh media session per call. Tests override this.
final callMediaFactoryProvider = Provider<CallMedia Function()>((ref) => LiveKitCallMedia.new);
