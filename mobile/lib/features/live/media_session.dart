import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:livekit_client/livekit_client.dart' as lk;

/// The only file that touches the LiveKit SDK, so screens and tests depend on
/// this small interface and never on WebRTC.
abstract class LiveMedia {
  /// Connects with a server-issued token. A publisher also turns on camera
  /// and microphone.
  Future<void> connect(String url, String token, {required bool publish});

  /// The video surface: everyone on camera (the host and any guests) in a
  /// grid. Rebuilds itself as tracks appear and disappear.
  Widget view();

  Future<void> disconnect();
}

/// Implemented by media sessions that can start and stop publishing after they
/// connected, which is what going on stage as a guest (and stepping down) needs.
abstract class StageMedia {
  Future<void> setPublishing(bool on);
}

class LiveKitMedia implements LiveMedia, StageMedia {
  final lk.Room _room = lk.Room();

  @override
  Future<void> connect(
    String url,
    String token, {
    required bool publish,
  }) async {
    await _room.connect(url, token);
    if (publish) await _enable(true);
  }

  Future<void> _enable(bool on) async {
    final me = _room.localParticipant;
    if (me == null) return;
    await me.setCameraEnabled(on);
    await me.setMicrophoneEnabled(on);
  }

  /// The server grants the publish permission just before the app is told, so
  /// the first try can be refused; a few short retries cover that gap.
  @override
  Future<void> setPublishing(bool on) async {
    Object? last;
    for (var i = 0; i < 4; i++) {
      try {
        await _enable(on);
        return;
      } catch (e) {
        last = e;
        await Future<void>.delayed(const Duration(milliseconds: 800));
      }
    }
    if (on) throw last ?? StateError('could not start the camera');
  }

  List<lk.VideoTrack> _tracks() {
    final out = <lk.VideoTrack>[];
    final people = <lk.Participant>[
      ..._room.remoteParticipants.values,
      if (_room.localParticipant != null) _room.localParticipant!,
    ];
    for (final p in people) {
      for (final pub in p.videoTrackPublications) {
        final t = pub.track;
        if (t is lk.VideoTrack && !pub.muted) out.add(t);
      }
    }
    return out;
  }

  @override
  Widget view() => ListenableBuilder(
    listenable: _room,
    builder: (context, _) {
      final tracks = _tracks();
      if (tracks.isEmpty) {
        return const ColoredBox(
          color: Colors.black,
          child: Center(
            child: Text(
              'Waiting for video…',
              style: TextStyle(color: Colors.white70),
            ),
          ),
        );
      }
      return CameraGrid(
        children: [
          for (final t in tracks)
            lk.VideoTrackRenderer(t, fit: lk.VideoViewFit.cover),
        ],
      );
    },
  );

  @override
  Future<void> disconnect() async {
    await _room.disconnect();
    await _room.dispose();
  }
}

/// Lays out one to eight camera tiles: full screen for one, then two columns,
/// then three, so a room with the host and seven guests still fits a phone.
class CameraGrid extends StatelessWidget {
  const CameraGrid({super.key, required this.children});
  final List<Widget> children;

  /// Columns used for [n] tiles.
  static int columnsFor(int n) => n <= 1 ? 1 : (n <= 4 ? 2 : 3);

  @override
  Widget build(BuildContext context) {
    final n = children.length;
    if (n == 1) return children.first;
    final cols = columnsFor(n);
    final rows = (n + cols - 1) ~/ cols;
    return LayoutBuilder(
      builder: (context, box) {
        final w = box.maxWidth / cols, h = box.maxHeight / rows;
        return Wrap(
          children: [
            for (final c in children)
              SizedBox(
                width: w,
                height: h,
                child: Padding(padding: const EdgeInsets.all(1), child: ClipRect(child: c)),
              ),
          ],
        );
      },
    );
  }
}

/// Creates a fresh media session per screen. Tests override this.
final liveMediaFactoryProvider = Provider<LiveMedia Function()>(
  (ref) => LiveKitMedia.new,
);
