import 'package:flutter/material.dart';

import '../../l10n/app_localizations.dart';

import '../../core/theme/tokens.dart';

/// The mark: three overlapping lights, one per visibility mode.
class BrandMark extends StatelessWidget {
  const BrandMark({super.key, this.size = 72});
  final double size;

  @override
  Widget build(BuildContext context) {
    final d = size * 0.62;
    Widget orb(ModeLight l, Alignment at) => Align(
          alignment: at,
          child: Container(
            width: d,
            height: d,
            decoration: BoxDecoration(shape: BoxShape.circle, gradient: l.gradient.scale(0.92)),
          ),
        );
    return SizedBox(
      width: size,
      height: size,
      child: Stack(
        children: [
          orb(ModeLight.dusk, Alignment.bottomRight),
          orb(ModeLight.lamplight, Alignment.bottomLeft),
          // Daylight on top, drawn translucent so the lights read as overlapping.
          Opacity(opacity: 0.88, child: orb(ModeLight.daylight, Alignment.topCenter)),
        ],
      ),
    );
  }
}

/// Sign-in hero: the mark, the wordmark and the promise, on soft pools of the
/// three mode lights.
class BrandHero extends StatelessWidget {
  const BrandHero({super.key});

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return Column(
      children: [
        const BrandMark(size: 84),
        const SizedBox(height: 20),
        Text('ConnectMe', style: t.textTheme.displaySmall, textAlign: TextAlign.center),
        const SizedBox(height: 8),
        Text(
          AppLocalizations.of(context)?.brandTagline ?? 'Be seen only as much as you choose.',
          style: t.textTheme.bodyMedium?.copyWith(color: t.colorScheme.onSurfaceVariant),
          textAlign: TextAlign.center,
        ),
      ],
    );
  }
}

/// Soft light behind a screen's top area (sign-in, onboarding).
class LightWash extends StatelessWidget {
  const LightWash({super.key, required this.child});
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final dark = Theme.of(context).brightness == Brightness.dark;
    final a = dark ? 0.32 : 0.45;
    RadialGradient pool(Alignment at, Color c, double r) =>
        RadialGradient(center: at, radius: r, colors: [c.withValues(alpha: a), c.withValues(alpha: 0)]);
    return Stack(
      children: [
        Positioned.fill(
          child: DecoratedBox(
            decoration: BoxDecoration(
              gradient: pool(const Alignment(-1.0, -1.0), ModeLight.daylight.start, 0.95),
            ),
          ),
        ),
        Positioned.fill(
          child: DecoratedBox(
            decoration: BoxDecoration(gradient: pool(const Alignment(1.0, -0.7), ModeLight.dusk.end, 0.8)),
          ),
        ),
        Positioned.fill(
          child: DecoratedBox(
            decoration: BoxDecoration(gradient: pool(const Alignment(0.2, 1.1), ModeLight.lamplight.start, 0.7)),
          ),
        ),
        child,
      ],
    );
  }
}
