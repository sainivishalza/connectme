import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/models/profile.dart';
import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/avatar.dart';
import '../profile_repository.dart';

/// The member's photos as a row they can arrange: the first photo is the one
/// shown on cards, each can carry a short caption, and any can be moved,
/// made the main photo or removed from a menu on the photo itself.
class PhotoManager extends ConsumerWidget {
  const PhotoManager({super.key, required this.photos, required this.onAdd, required this.onChanged});
  final List<OwnPhoto> photos;
  final VoidCallback onAdd;
  final VoidCallback onChanged;

  Future<void> _run(BuildContext context, Future<void> Function() f) async {
    if (await guarded(context, f)) onChanged();
  }

  Future<void> _move(BuildContext context, WidgetRef ref, int from, int to) {
    final ids = [for (final p in photos) p.id];
    ids.insert(to, ids.removeAt(from));
    return _run(context, () => ref.read(profileRepositoryProvider).reorderPhotos(ids));
  }

  Future<void> _caption(BuildContext context, WidgetRef ref, OwnPhoto ph) async {
    final text = await showDialog<String>(context: context, builder: (_) => _CaptionDialog(initial: ph.caption));
    if (text == null || !context.mounted) return;
    await _run(context, () => ref.read(profileRepositoryProvider).setPhotoCaption(ph.id, text));
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final t = Theme.of(context);
    return SizedBox(
      height: photos.isEmpty ? 96 : 168,
      child: ListView(
        scrollDirection: Axis.horizontal,
        children: [
          for (var i = 0; i < photos.length; i++)
            Padding(
              key: Key('photo_tile_$i'),
              padding: const EdgeInsets.only(right: 10),
              child: SizedBox(
                width: 96,
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Stack(
                      clipBehavior: Clip.none,
                      children: [
                        Avatar(photoPath: photos[i].path, radius: 44),
                        if (i == 0)
                          Positioned(
                            left: 0,
                            bottom: 0,
                            child: DecoratedBox(
                              key: const Key('main_badge'),
                              decoration: BoxDecoration(color: t.colorScheme.primary, borderRadius: BorderRadius.circular(10)),
                              child: Padding(
                                padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
                                child: Text('Main', style: t.textTheme.labelSmall?.copyWith(color: t.colorScheme.onPrimary)),
                              ),
                            ),
                          ),
                        Positioned(
                          right: -10,
                          top: -10,
                          child: PopupMenuButton<String>(
                            key: Key('photo_menu_$i'),
                            tooltip: 'Photo options',
                            icon: const Icon(Icons.more_vert),
                            onSelected: (v) {
                              switch (v) {
                                case 'main':
                                  _run(context, () => ref.read(profileRepositoryProvider).makeMainPhoto(photos[i].id));
                                case 'left':
                                  _move(context, ref, i, i - 1);
                                case 'right':
                                  _move(context, ref, i, i + 1);
                                case 'caption':
                                  _caption(context, ref, photos[i]);
                                case 'remove':
                                  _run(context, () => ref.read(profileRepositoryProvider).deletePhoto(photos[i].id));
                              }
                            },
                            itemBuilder: (_) => [
                              if (i > 0) const PopupMenuItem(value: 'main', child: Text('Make main photo')),
                              if (i > 0) const PopupMenuItem(value: 'left', child: Text('Move earlier')),
                              if (i < photos.length - 1) const PopupMenuItem(value: 'right', child: Text('Move later')),
                              const PopupMenuItem(value: 'caption', child: Text('Edit caption')),
                              const PopupMenuItem(value: 'remove', child: Text('Remove photo')),
                            ],
                          ),
                        ),
                      ],
                    ),
                    const SizedBox(height: 6),
                    Text(
                      photos[i].caption.isEmpty ? 'No caption' : photos[i].caption,
                      key: Key('photo_caption_$i'),
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: t.textTheme.bodySmall?.copyWith(color: photos[i].caption.isEmpty ? t.colorScheme.onSurfaceVariant : null),
                    ),
                  ],
                ),
              ),
            ),
          if (photos.length < 6)
            Align(
              alignment: Alignment.topCenter,
              child: Semantics(
                button: true,
                label: 'Add photo',
                child: InkWell(
                  key: const Key('add_photo'),
                  customBorder: const CircleBorder(),
                  onTap: onAdd,
                  child: const CircleAvatar(radius: 44, child: Icon(Icons.add_a_photo_outlined)),
                ),
              ),
            ),
        ],
      ),
    );
  }
}

class _CaptionDialog extends StatefulWidget {
  const _CaptionDialog({required this.initial});
  final String initial;

  @override
  State<_CaptionDialog> createState() => _CaptionDialogState();
}

class _CaptionDialogState extends State<_CaptionDialog> {
  late final _c = TextEditingController(text: widget.initial);

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('Photo caption'),
      content: TextField(
        key: const Key('caption_field'),
        controller: _c,
        maxLength: 100,
        autofocus: true,
        decoration: const InputDecoration(hintText: 'A few words about this photo'),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.pop(context), child: const Text('Cancel')),
        FilledButton(key: const Key('caption_save'), onPressed: () => Navigator.pop(context, _c.text.trim()), child: const Text('Save')),
      ],
    );
  }
}
