import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:video_player/video_player.dart';

import '../videos_repository.dart';

/// Builds the player for a clip id. Tests replace this so no codec is needed.
typedef VideoViewBuilder = Widget Function(String clipId, {bool autoplay});

final videoViewBuilderProvider = Provider<VideoViewBuilder>(
  (ref) => (id, {autoplay = true}) => ClipPlayer(key: ValueKey('player_$id'), clipId: id, autoplay: autoplay),
);

/// Fetches a signed URL for the clip and plays it in a loop; tap to pause.
class ClipPlayer extends ConsumerStatefulWidget {
  const ClipPlayer({super.key, required this.clipId, this.autoplay = true});
  final String clipId;
  final bool autoplay;

  @override
  ConsumerState<ClipPlayer> createState() => _ClipPlayerState();
}

class _ClipPlayerState extends ConsumerState<ClipPlayer> {
  VideoPlayerController? _c;
  Object? _error;

  @override
  void initState() {
    super.initState();
    _init();
  }

  Future<void> _init() async {
    try {
      final url = await ref.read(videosRepositoryProvider).playUrl(widget.clipId);
      final c = VideoPlayerController.networkUrl(Uri.parse(url));
      await c.initialize();
      await c.setLooping(true);
      if (!mounted) {
        await c.dispose();
        return;
      }
      setState(() => _c = c);
      if (widget.autoplay) await c.play();
    } catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  @override
  void dispose() {
    _c?.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final c = _c;
    if (_error != null) {
      return const Center(child: Text('This video cannot be played right now.', style: TextStyle(color: Colors.white70)));
    }
    if (c == null) return const Center(child: CircularProgressIndicator());
    return GestureDetector(
      behavior: HitTestBehavior.opaque,
      onTap: () => setState(() => c.value.isPlaying ? c.pause() : c.play()),
      child: Stack(alignment: Alignment.center, children: [
        FittedBox(
          fit: BoxFit.cover,
          clipBehavior: Clip.hardEdge,
          child: SizedBox(width: c.value.size.width, height: c.value.size.height, child: VideoPlayer(c)),
        ),
        if (!c.value.isPlaying) const Icon(Icons.play_arrow_rounded, size: 72, color: Colors.white70),
      ]),
    );
  }
}

/// Plays a clip in a dialog (profile reels).
Future<void> showClipDialog(BuildContext context, WidgetRef ref, String clipId) {
  return showDialog<void>(
    context: context,
    builder: (c) => Dialog(
      clipBehavior: Clip.antiAlias,
      backgroundColor: Colors.black,
      child: AspectRatio(
        aspectRatio: 9 / 16,
        child: Stack(children: [
          Positioned.fill(child: ref.read(videoViewBuilderProvider)(clipId)),
          Positioned(
            top: 4,
            right: 4,
            child: IconButton(
              key: const Key('close_clip'),
              color: Colors.white,
              icon: const Icon(Icons.close),
              onPressed: () => Navigator.pop(c),
            ),
          ),
        ]),
      ),
    ),
  );
}
