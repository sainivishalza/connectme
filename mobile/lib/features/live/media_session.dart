import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:livekit_client/livekit_client.dart' as lk;

/// The only file that touches the LiveKit SDK, so screens and tests depend on
/// this small interface and never on WebRTC.
abstract class LiveMedia {
  /// Connects with a server-issued token. A publisher also turns on camera
  /// and microphone.
  Future<void> connect(String url, String token, {required bool publish});

  /// The video surface (local preview for a broadcaster, the host's track for
  /// a viewer). Rebuilds itself as tracks appear and disappear.
  Widget view();

  Future<void> disconnect();
}

class LiveKitMedia implements LiveMedia {
  final lk.Room _room = lk.Room();
  bool _publish = false;

  @override
  Future<void> connect(String url, String token, {required bool publish}) async {
    _publish = publish;
    await _room.connect(url, token);
    if (publish) {
      await _room.localParticipant?.setCameraEnabled(true);
      await _room.localParticipant?.setMicrophoneEnabled(true);
    }
  }

  lk.VideoTrack? _track() {
    final participants = <lk.Participant>[
      if (_publish && _room.localParticipant != null) _room.localParticipant!,
      if (!_publish) ..._room.remoteParticipants.values,
    ];
    for (final p in participants) {
      for (final pub in p.videoTrackPublications) {
        final t = pub.track;
        if (t is lk.VideoTrack && !pub.muted) return t;
      }
    }
    return null;
  }

  @override
  Widget view() => ListenableBuilder(
        listenable: _room,
        builder: (context, _) {
          final t = _track();
          if (t == null) {
            return const ColoredBox(
              color: Colors.black,
              child: Center(child: Text('Waiting for video…', style: TextStyle(color: Colors.white70))),
            );
          }
          return lk.VideoTrackRenderer(t, fit: lk.VideoViewFit.cover);
        },
      );

  @override
  Future<void> disconnect() async {
    await _room.disconnect();
    await _room.dispose();
  }
}

/// Creates a fresh media session per screen. Tests override this.
final liveMediaFactoryProvider = Provider<LiveMedia Function()>((ref) => LiveKitMedia.new);
