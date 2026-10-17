import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/providers.dart';
import '../../core/theme/tokens.dart';

/// Authenticated photo bytes. Photos are served only to people allowed to see
/// them, so they are fetched through the API client (token + refresh) and not
/// by a bare URL.
final photoBytesProvider = FutureProvider.autoDispose.family<Uint8List, String>((ref, path) {
  return ref.watch(apiClientProvider).getBytes(path);
});

/// Round avatar: the photo when visible, otherwise initials, or a mask icon
/// for anonymous (incognito / hotel) profiles.
class Avatar extends ConsumerWidget {
  const Avatar({super.key, this.name, this.photoPath, this.anonymous = false, this.radius = 24, this.mode});

  final String? name;
  final String? photoPath;
  final bool anonymous;
  final double radius;

  /// When set (open | incognito | hotel) the avatar wears that mode's light as
  /// a ring, so the person's visibility is readable at a glance.
  final String? mode;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final scheme = Theme.of(context).colorScheme;
    final light = mode == null ? null : ModeLight.of(mode!);
    Widget fallback() {
      if (anonymous) {
        // A veil, not a grey placeholder: the mode's light with a closed eye.
        final v = light ?? ModeLight.dusk;
        return DecoratedBox(
          decoration: BoxDecoration(shape: BoxShape.circle, gradient: v.gradient),
          child: SizedBox.expand(child: Icon(Icons.visibility_off_outlined, size: radius * 0.9, color: v.ink)),
        );
      }
      final initial = (name ?? '').trim().isEmpty ? '?' : name!.trim()[0].toUpperCase();
      return Text(initial,
          style: TextStyle(fontFamily: Fonts.display, fontSize: radius * 0.95, color: light == null ? scheme.onSecondaryContainer : scheme.onSurface));
    }

    final path = photoPath;
    final core = CircleAvatar(
      radius: radius,
      backgroundColor: light == null
          ? scheme.secondaryContainer
          : Color.alphaBlend(light.solid.withValues(alpha: 0.16), scheme.surface),
      child: path == null
          ? fallback()
          : ref.watch(photoBytesProvider(path)).when(
                data: (b) => ClipOval(child: Image.memory(b, width: radius * 2, height: radius * 2, fit: BoxFit.cover)),
                loading: () => SizedBox(width: radius, height: radius, child: const CircularProgressIndicator(strokeWidth: 2)),
                error: (_, _) => fallback(),
              ),
    );
    if (light == null) return core;
    final ring = (radius * 0.08).clamp(2.0, 4.0);
    return Container(
      padding: EdgeInsets.all(ring),
      decoration: BoxDecoration(shape: BoxShape.circle, gradient: light.gradient),
      child: Container(
        padding: const EdgeInsets.all(2.5),
        decoration: BoxDecoration(shape: BoxShape.circle, color: scheme.surface),
        child: core,
      ),
    );
  }
}
