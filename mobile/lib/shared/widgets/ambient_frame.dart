import 'package:flutter/material.dart';

import '../../core/theme/tokens.dart';

/// On wide screens (web, tablets) the app is a phone-width column floating on
/// a soft field made of the three mode lights, with the idea of the product
/// beside it. On phones this widget does nothing.
class AmbientFrame extends StatelessWidget {
  const AmbientFrame({super.key, required this.child});
  final Widget child;

  static const _columnWidth = 460.0;
  static const _wide = 760.0;
  static const _withCopy = 1080.0;

  @override
  Widget build(BuildContext context) {
    final mq = MediaQuery.of(context);
    final w = mq.size.width;
    if (w < _wide) return child;

    final t = Theme.of(context);
    final colH = (mq.size.height - 40).clamp(520.0, 940.0);
    final column = Center(
      child: Container(
        width: _columnWidth,
        height: colH,
        decoration: BoxDecoration(
          color: t.colorScheme.surface,
          borderRadius: BorderRadius.circular(40),
          border: Border.all(color: t.colorScheme.outlineVariant),
          boxShadow: [
            BoxShadow(
              color: Palette.midnight.withValues(alpha: t.brightness == Brightness.light ? 0.16 : 0.5),
              blurRadius: 60,
              offset: const Offset(0, 24),
            ),
          ],
        ),
        clipBehavior: Clip.antiAlias,
        // The column is the app's real viewport, so widgets that read the
        // media size lay out for it rather than for the whole window.
        child: MediaQuery(data: mq.copyWith(size: Size(_columnWidth, colH)), child: child),
      ),
    );

    return ColoredBox(
      color: t.brightness == Brightness.light ? Palette.porcelain : Palette.night,
      child: Stack(
        children: [
          const Positioned.fill(child: _LightField()),
          if (w >= _withCopy)
            Positioned.fill(
              child: Row(
                children: [
                  Expanded(
                    child: Align(
                      alignment: Alignment.centerRight,
                      child: Padding(padding: const EdgeInsets.only(right: 72), child: _Pitch()),
                    ),
                  ),
                  column,
                  const Expanded(child: SizedBox()),
                ],
              ),
            )
          else
            Positioned.fill(child: column),
        ],
      ),
    );
  }
}

class _Pitch extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return ConstrainedBox(
      constraints: const BoxConstraints(maxWidth: 380),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('ConnectMe', style: t.textTheme.titleLarge?.copyWith(color: t.colorScheme.onSurfaceVariant)),
          const SizedBox(height: 14),
          Text('Be seen only as much as you choose.', style: t.textTheme.displaySmall),
          const SizedBox(height: 18),
          Text(
            'Open, Incognito or Hotel: you decide what each person sees, and you can change it whenever you like.',
            style: t.textTheme.bodyLarge?.copyWith(color: t.colorScheme.onSurfaceVariant),
          ),
        ],
      ),
    );
  }
}

/// Three slow pools of light, one per mode, behind the column.
class _LightField extends StatelessWidget {
  const _LightField();

  @override
  Widget build(BuildContext context) {
    final dark = Theme.of(context).brightness == Brightness.dark;
    final a = dark ? 0.30 : 0.42;
    Widget pool(Alignment at, Color c, double radius) => Positioned.fill(
          child: DecoratedBox(
            decoration: BoxDecoration(
              gradient: RadialGradient(center: at, radius: radius, colors: [c.withValues(alpha: a), c.withValues(alpha: 0)]),
            ),
          ),
        );
    return Stack(
      children: [
        pool(const Alignment(-0.9, -0.8), ModeLight.daylight.start, 0.9),
        pool(const Alignment(0.95, 0.9), ModeLight.dusk.end, 1.0),
        pool(const Alignment(0.85, -0.85), ModeLight.lamplight.start, 0.55),
      ],
    );
  }
}
