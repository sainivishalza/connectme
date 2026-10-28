import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/models.dart';
import '../../../core/theme/tokens.dart';
import '../../../shared/widgets/avatar.dart';

/// A person's photos, one at a time, the way dating apps show them: segment
/// bars along the top, tap the right edge for the next photo and the left edge
/// for the previous one. When the server sends no photo (anonymous profiles, or
/// none uploaded) it shows the light of the person's mode with a veil instead.
class PhotoCarousel extends ConsumerStatefulWidget {
  const PhotoCarousel({super.key, required this.profile, required this.light, this.onCenterTap, this.child});

  final PublicProfile profile;
  final ModeLight light;

  /// Tapping the middle of the photo (not an edge) does this.
  final VoidCallback? onCenterTap;

  /// Drawn on top of the photo (name, chips, stamps).
  final Widget? child;

  @override
  ConsumerState<PhotoCarousel> createState() => _PhotoCarouselState();
}

class _PhotoCarouselState extends ConsumerState<PhotoCarousel> {
  int _index = 0;

  List<String> get _photos => widget.profile.photos;

  void _go(int delta) {
    final next = (_index + delta).clamp(0, _photos.length - 1);
    if (next == _index) return;
    setState(() => _index = next);
    // Warm the following photo so the next tap is instant.
    if (next + 1 < _photos.length) ref.read(photoBytesProvider(_photos[next + 1]));
  }

  @override
  void didUpdateWidget(PhotoCarousel old) {
    super.didUpdateWidget(old);
    if (old.profile.ref != widget.profile.ref) _index = 0;
  }

  @override
  Widget build(BuildContext context) {
    final p = widget.profile;
    final t = Theme.of(context);
    final hasPhotos = _photos.isNotEmpty;
    final path = hasPhotos ? _photos[_index.clamp(0, _photos.length - 1)] : null;
    final photo = path == null
        ? null
        : ref.watch(photoBytesProvider(path)).maybeWhen(
              data: (b) => Image.memory(b, key: ValueKey(path), fit: BoxFit.cover, width: double.infinity, height: double.infinity, gaplessPlayback: true),
              orElse: () => null,
            );
    return LayoutBuilder(builder: (context, box) {
      return GestureDetector(
        behavior: HitTestBehavior.opaque,
        onTapUp: (d) {
          final x = d.localPosition.dx / box.maxWidth;
          if (hasPhotos && _photos.length > 1 && x < 0.3) {
            _go(-1);
          } else if (hasPhotos && _photos.length > 1 && x > 0.7) {
            _go(1);
          } else {
            widget.onCenterTap?.call();
          }
        },
        child: Stack(fit: StackFit.expand, children: [
          if (photo != null)
            photo
          else
            DecoratedBox(decoration: BoxDecoration(gradient: widget.light.gradient)),
          if (photo == null)
            Center(child: Avatar(name: p.displayName, anonymous: p.anonymous, radius: 52, mode: p.mode)),
          if (hasPhotos && photo == null) const Center(child: CircularProgressIndicator(strokeWidth: 2)),
          if (p.anonymous && !hasPhotos)
            Positioned(
              left: 22,
              right: 22,
              top: box.maxHeight * 0.58,
              child: Text('Face hidden until you both reveal',
                  textAlign: TextAlign.center, style: t.textTheme.labelLarge?.copyWith(color: widget.light.ink.withValues(alpha: 0.92))),
            ),
          if (_photos.length > 1)
            Positioned(
              top: 10,
              left: 12,
              right: 12,
              child: Row(children: [
                for (var i = 0; i < _photos.length; i++)
                  Expanded(
                    child: Container(
                      key: Key('photo_bar_$i'),
                      height: 3.5,
                      margin: const EdgeInsets.symmetric(horizontal: 2),
                      decoration: BoxDecoration(
                        color: i == _index ? Colors.white : Colors.white.withValues(alpha: 0.38),
                        borderRadius: BorderRadius.circular(2),
                      ),
                    ),
                  ),
              ]),
            ),
          if (hasPhotos && _index < p.photoCaptions.length && p.photoCaptions[_index].isNotEmpty)
            Positioned(
              top: _photos.length > 1 ? 24 : 12,
              left: 14,
              right: 14,
              child: Align(
                alignment: Alignment.centerLeft,
                child: DecoratedBox(
                  key: const Key('photo_caption'),
                  decoration: BoxDecoration(color: Colors.black.withValues(alpha: 0.55), borderRadius: BorderRadius.circular(14)),
                  child: Padding(
                    padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 5),
                    child: Text(p.photoCaptions[_index], maxLines: 2, overflow: TextOverflow.ellipsis, style: t.textTheme.labelLarge?.copyWith(color: Colors.white)),
                  ),
                ),
              ),
            ),
          if (widget.child != null) widget.child!,
        ]),
      );
    });
  }
}
