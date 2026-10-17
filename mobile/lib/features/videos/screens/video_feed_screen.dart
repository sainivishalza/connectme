import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../videos_repository.dart';
import '../widgets/video_view.dart';

/// Short videos: swipe up for the next clip. Only Open-mode members post here.
class VideoFeedScreen extends ConsumerStatefulWidget {
  const VideoFeedScreen({super.key});

  @override
  ConsumerState<VideoFeedScreen> createState() => _VideoFeedScreenState();
}

class _VideoFeedScreenState extends ConsumerState<VideoFeedScreen> {
  late final VideosRepository _repo;
  final _pages = PageController();
  List<VideoClip> _clips = [];
  Object? _error;
  bool _loading = true;
  bool _more = true;
  bool _loadingMore = false;
  int _index = 0;

  @override
  void initState() {
    super.initState();
    _repo = ref.read(videosRepositoryProvider);
    _load();
  }

  @override
  void dispose() {
    _pages.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final cs = await _repo.feed();
      if (mounted) {
        setState(() {
          _clips = cs;
          _more = cs.length >= 15;
          _error = null;
          _loading = false;
        });
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _error = e;
          _loading = false;
        });
      }
    }
  }

  Future<void> _loadMore() async {
    if (!_more || _loadingMore || _clips.isEmpty) return;
    _loadingMore = true;
    try {
      final cs = await _repo.feed(before: _clips.last.createdAt);
      if (mounted) {
        setState(() {
          _clips = [..._clips, ...cs];
          _more = cs.length >= 15;
        });
      }
    } catch (_) {
      // the next swipe retries
    }
    _loadingMore = false;
  }

  Future<void> _like(int i) async {
    final c = _clips[i];
    try {
      final r = await _repo.like(c.id);
      if (mounted) setState(() => _clips[i] = c.copyWith(likes: r.likes, liked: r.liked));
    } catch (e) {
      if (mounted) showError(context, e);
    }
  }

  Future<void> _delete(int i) async {
    if (await guarded(context, () => _repo.delete(_clips[i].id))) {
      if (mounted) setState(() => _clips.removeAt(i));
    }
  }

  Future<void> _report(VideoClip c) async {
    final reason = await showModalBottomSheet<String>(
      context: context,
      builder: (ctx) => SafeArea(
        child: Column(mainAxisSize: MainAxisSize.min, children: [
          const ListTile(title: Text('Report this video')),
          for (final r in const {
            'inappropriate_content': 'Inappropriate content',
            'harassment': 'Harassment',
            'scam': 'Scam',
            'underage': 'Looks underage',
            'spam': 'Spam',
          }.entries)
            ListTile(key: Key('report_${r.key}'), title: Text(r.value), onTap: () => Navigator.pop(ctx, r.key)),
        ]),
      ),
    );
    if (reason == null || !mounted) return;
    if (await guarded(context, () => _repo.report(c.id, reason)) && mounted) {
      showMessage(context, 'Thanks. Our team will take a look.');
    }
  }

  Future<void> _post() async {
    final pick = ref.read(videoPickerProvider);
    final choice = await showModalBottomSheet<bool>(
      context: context,
      builder: (c) => SafeArea(
        child: Column(mainAxisSize: MainAxisSize.min, children: [
          ListTile(key: const Key('pick_camera'), leading: const Icon(Icons.videocam_outlined), title: const Text('Record a video'), onTap: () => Navigator.pop(c, true)),
          ListTile(key: const Key('pick_gallery'), leading: const Icon(Icons.video_library_outlined), title: const Text('Choose from your library'), onTap: () => Navigator.pop(c, false)),
        ]),
      ),
    );
    if (choice == null) return;
    final picked = await pick(camera: choice);
    if (picked == null || !mounted) return;
    final caption = await _askCaption();
    if (caption == null || !mounted) return;
    VideoClip? made;
    if (await guarded(context, () async => made = await _repo.upload('short', picked.bytes, caption: caption)) && mounted) {
      showMessage(context, made?.held == true ? 'Posted. It appears in the feed after a quick review.' : 'Posted');
      setState(() => _loading = true);
      await _load();
    }
  }

  Future<String?> _askCaption() {
    final ctl = TextEditingController();
    return showDialog<String>(
      context: context,
      builder: (c) => AlertDialog(
        title: const Text('Add a caption'),
        content: TextField(key: const Key('video_caption'), controller: ctl, maxLength: 150, decoration: const InputDecoration(hintText: 'Optional')),
        actions: [
          TextButton(onPressed: () => Navigator.pop(c), child: const Text('Cancel')),
          FilledButton(key: const Key('video_post'), onPressed: () => Navigator.pop(c, ctl.text.trim()), child: const Text('Post')),
        ],
      ),
    ).whenComplete(() => Future<void>.delayed(const Duration(milliseconds: 500), ctl.dispose));
  }

  @override
  Widget build(BuildContext context) {
    Widget body;
    if (_loading) {
      body = const Loader();
    } else if (_error != null) {
      body = ErrorView(error: _error!, onRetry: () {
        setState(() => _loading = true);
        _load();
      });
    } else if (_clips.isEmpty) {
      body = EmptyState(
        icon: Icons.smart_display_outlined,
        title: 'No videos yet',
        message: 'Post a clip of up to 30 seconds. Only Open-mode members can post.',
        action: FilledButton(onPressed: _post, child: const Text('Post a video')),
      );
    } else {
      body = PageView.builder(
        controller: _pages,
        scrollDirection: Axis.vertical,
        itemCount: _clips.length,
        onPageChanged: (i) {
          setState(() => _index = i);
          if (i >= _clips.length - 3) _loadMore();
        },
        itemBuilder: (c, i) => _Page(
          clip: _clips[i],
          active: i == _index,
          onLike: () => _like(i),
          onReport: () => _report(_clips[i]),
          onDelete: () => _delete(i),
        ),
      );
    }
    return Scaffold(
      backgroundColor: Colors.black,
      extendBodyBehindAppBar: true,
      appBar: AppBar(
        backgroundColor: Colors.transparent,
        foregroundColor: Colors.white,
        title: const Text('Videos'),
        actions: [IconButton(key: const Key('post_video'), tooltip: 'Post a video', icon: const Icon(Icons.add_box_outlined), onPressed: _post)],
      ),
      body: body,
    );
  }
}

class _Page extends ConsumerWidget {
  const _Page({required this.clip, required this.active, required this.onLike, required this.onReport, required this.onDelete});
  final VideoClip clip;
  final bool active;
  final VoidCallback onLike;
  final VoidCallback onReport;
  final VoidCallback onDelete;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final t = Theme.of(context);
    const shadow = [Shadow(blurRadius: 6, color: Colors.black87)];
    return Stack(fit: StackFit.expand, children: [
      if (active) ref.read(videoViewBuilderProvider)(clip.id) else const ColoredBox(color: Colors.black),
      const DecoratedBox(
        decoration: BoxDecoration(gradient: LinearGradient(begin: Alignment.center, end: Alignment.bottomCenter, colors: [Colors.transparent, Colors.black54])),
      ),
      Positioned(
        left: 16,
        right: 88,
        bottom: 32,
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, mainAxisSize: MainAxisSize.min, children: [
          Text(clip.mine ? 'You' : clip.authorName, style: t.textTheme.titleMedium?.copyWith(color: Colors.white, shadows: shadow)),
          if (clip.caption.isNotEmpty) ...[
            const SizedBox(height: 6),
            Text(clip.caption, maxLines: 3, overflow: TextOverflow.ellipsis, style: t.textTheme.bodyMedium?.copyWith(color: Colors.white, shadows: shadow)),
          ],
        ]),
      ),
      Positioned(
        right: 8,
        bottom: 24,
        child: Column(mainAxisSize: MainAxisSize.min, children: [
          IconButton(
            key: Key('like_${clip.id}'),
            iconSize: 32,
            color: clip.liked ? Colors.redAccent : Colors.white,
            icon: Icon(clip.liked ? Icons.favorite : Icons.favorite_border),
            onPressed: onLike,
          ),
          Text('${clip.likes}', style: t.textTheme.labelLarge?.copyWith(color: Colors.white, shadows: shadow)),
          const SizedBox(height: 8),
          if (clip.mine)
            IconButton(key: Key('delete_${clip.id}'), color: Colors.white, icon: const Icon(Icons.delete_outline), onPressed: onDelete)
          else
            IconButton(key: Key('report_${clip.id}'), color: Colors.white, icon: const Icon(Icons.flag_outlined), onPressed: onReport),
        ]),
      ),
    ]);
  }
}
