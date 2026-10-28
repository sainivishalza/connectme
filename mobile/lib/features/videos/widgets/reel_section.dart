import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../shared/widgets/app_snack.dart';
import '../videos_repository.dart';
import 'video_view.dart';

/// Your profile video: a short clip people who can see your full profile may
/// watch. It stays hidden while you are anonymous, like your photos.
class ReelSection extends ConsumerStatefulWidget {
  const ReelSection({super.key, required this.myRef});
  final String myRef;

  @override
  ConsumerState<ReelSection> createState() => _ReelSectionState();
}

class _ReelSectionState extends ConsumerState<ReelSection> {
  Future<void> _pick(bool camera) async {
    final picked = await ref.read(videoPickerProvider)(camera: camera);
    if (picked == null || !mounted) return;
    VideoClip? saved;
    final ok = await guarded(context, () async => saved = await ref.read(videosRepositoryProvider).upload('reel', picked.bytes));
    if (ok) {
      ref.invalidate(reelProvider(widget.myRef));
      if (mounted) showMessage(context, saved?.held == true ? 'Saved. It shows on your profile after a quick review.' : 'Video saved');
    }
  }

  Future<void> _remove(String id) async {
    final ok = await guarded(context, () => ref.read(videosRepositoryProvider).delete(id));
    if (ok) ref.invalidate(reelProvider(widget.myRef));
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final reel = ref.watch(reelProvider(widget.myRef));
    final clip = reel.value;
    return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
      Text('Video reel', style: t.textTheme.titleMedium),
      const SizedBox(height: 4),
      Text(
        'Up to 30 seconds. Only people who can see your full profile can watch it; it stays hidden while you are anonymous.',
        style: t.textTheme.bodySmall,
      ),
      if (clip != null && clip.held) ...[
        const SizedBox(height: 8),
        Text('Under review: only you can see it until a moderator approves it.', key: const Key('reel_held'), style: t.textTheme.bodySmall?.copyWith(color: t.colorScheme.primary)),
      ],
      const SizedBox(height: 10),
      Wrap(spacing: 8, runSpacing: 8, children: [
        if (clip != null) ...[
          OutlinedButton.icon(
            key: const Key('watch_reel'),
            onPressed: () => showClipDialog(context, ref, clip.id),
            icon: const Icon(Icons.play_circle_outline),
            label: Text('Watch (${clip.seconds}s)'),
          ),
          TextButton(key: const Key('remove_reel'), onPressed: () => _remove(clip.id), child: Text(AppLocalizations.of(context)?.sRemove ?? 'Remove')),
        ],
        FilledButton.tonalIcon(
          key: const Key('add_reel'),
          onPressed: () => _pick(false),
          icon: const Icon(Icons.video_library_outlined),
          label: Text(clip == null ? 'Add a video' : 'Replace'),
        ),
        OutlinedButton.icon(key: const Key('record_reel'), onPressed: () => _pick(true), icon: const Icon(Icons.videocam_outlined), label: Text(AppLocalizations.of(context)?.sRecord ?? 'Record')),
      ]),
    ]);
  }
}
