import 'package:flutter/material.dart';

import '../../../core/theme/tokens.dart';

/// The row of round buttons under the deck: rewind, pass, super like, like.
class DeckActions extends StatelessWidget {
  const DeckActions({super.key, required this.onRewind, required this.onPass, required this.onSuperLike, required this.onLike});

  final VoidCallback? onRewind;
  final VoidCallback? onPass;
  final VoidCallback? onSuperLike;
  final VoidCallback? onLike;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return Row(mainAxisAlignment: MainAxisAlignment.spaceEvenly, crossAxisAlignment: CrossAxisAlignment.center, children: [
      _Round(key: const Key('rewind'), tooltip: 'Rewind', icon: Icons.replay, size: 52, color: t.brightness == Brightness.light ? const Color(0xFFB07F00) : const Color(0xFFE2A400), onPressed: onRewind),
      _Round(key: const Key('pass'), tooltip: 'Pass', icon: Icons.close, size: 62, color: const Color(0xFFE5484D), onPressed: onPass),
      _Round(key: const Key('superlike'), tooltip: 'Super Like', icon: Icons.star_rounded, size: 54, color: const Color(0xFF3E8BFF), onPressed: onSuperLike),
      _Round(key: const Key('like'), tooltip: 'Like', icon: Icons.favorite, size: 62, color: t.colorScheme.primary, filled: true, onPressed: onLike),
    ]);
  }
}

class _Round extends StatelessWidget {
  const _Round({super.key, required this.tooltip, required this.icon, required this.size, required this.color, required this.onPressed, this.filled = false});
  final String tooltip;
  final IconData icon;
  final double size;
  final Color color;
  final VoidCallback? onPressed;
  final bool filled;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return Container(
      width: size,
      height: size,
      decoration: BoxDecoration(
        shape: BoxShape.circle,
        gradient: filled ? ModeLight.daylight.gradient : null,
        color: filled ? null : t.colorScheme.surfaceContainerLow,
        border: filled ? null : Border.all(color: color.withValues(alpha: 0.55), width: 1.6),
        boxShadow: [BoxShadow(color: color.withValues(alpha: filled ? 0.35 : 0.14), blurRadius: filled ? 20 : 10, offset: const Offset(0, 6))],
      ),
      child: IconButton(
        tooltip: tooltip,
        onPressed: onPressed,
        iconSize: size * 0.46,
        color: filled ? ModeLight.daylight.ink : color,
        icon: Icon(icon),
        style: IconButton.styleFrom(minimumSize: Size.square(size), fixedSize: Size.square(size)),
      ),
    );
  }
}
