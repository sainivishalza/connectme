import 'dart:math' as math;

import 'package:connectme/core/theme/app_theme.dart';
import 'package:connectme/core/theme/tokens.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

/// WCAG 2.x contrast ratio between two opaque colours.
double contrast(Color a, Color b) {
  double lum(Color c) {
    double ch(double v) => v <= 0.03928 ? v / 12.92 : math.pow((v + 0.055) / 1.055, 2.4).toDouble();
    return 0.2126 * ch(c.r) + 0.7152 * ch(c.g) + 0.0722 * ch(c.b);
  }

  final la = lum(a), lb = lum(b);
  final hi = math.max(la, lb), lo = math.min(la, lb);
  return (hi + 0.05) / (lo + 0.05);
}

void main() {
  for (final b in [Brightness.light, Brightness.dark]) {
    final s = (b == Brightness.light ? AppTheme.light() : AppTheme.dark()).colorScheme;
    // Text pairs need 4.5:1 (WCAG AA for normal text).
    final text = <String, (Color, Color)>{
      'body on page': (s.onSurface, s.surface),
      'body on card': (s.onSurface, s.surfaceContainerLow),
      'secondary text on page': (s.onSurfaceVariant, s.surface),
      'secondary text on card': (s.onSurfaceVariant, s.surfaceContainerLow),
      'secondary text on quiet fill': (s.onSurfaceVariant, s.surfaceContainerHighest),
      'primary button': (s.onPrimary, s.primary),
      'on primary container': (s.onPrimaryContainer, s.primaryContainer),
      'on secondary container': (s.onSecondaryContainer, s.secondaryContainer),
      'on tertiary container': (s.onTertiaryContainer, s.tertiaryContainer),
      'error text on page': (s.error, s.surface),
      'text on error': (s.onError, s.error),
      'inverse text (snack bar)': (s.onInverseSurface, s.inverseSurface),
    };
    for (final e in text.entries) {
      test('${b.name}: ${e.key} is readable (4.5:1)', () {
        final r = contrast(e.value.$1, e.value.$2);
        expect(r, greaterThanOrEqualTo(4.5), reason: '${e.key} is only ${r.toStringAsFixed(2)}:1');
      });
    }
    // Controls and outlines need 3:1 against what they sit on (WCAG 1.4.11).
    test('${b.name}: input outline and icons are visible (3:1)', () {
      expect(contrast(s.outline, s.surface), greaterThanOrEqualTo(3.0));
      expect(contrast(s.outline, s.surfaceContainerLow), greaterThanOrEqualTo(3.0));
    });
    test('${b.name}: the secondary accent is visible as an icon or switch (3:1)', () {
      expect(contrast(s.secondary, s.surface), greaterThanOrEqualTo(3.0));
    });
  }

  group('mode lights', () {
    for (final m in [ModeLight.daylight, ModeLight.dusk, ModeLight.lamplight]) {
      test('${m.label}: ink is readable on both ends of its gradient', () {
        expect(contrast(m.ink, m.start), greaterThanOrEqualTo(4.5), reason: '${m.label} start');
        expect(contrast(m.ink, m.end), greaterThanOrEqualTo(4.5), reason: '${m.label} end');
      });
    }
  });

  group('overlay text on photos', () {
    // White text sits on a dark fade; the fade is at least 54% black over any photo.
    test('white on 54% black is readable even over a white photo', () {
      final worst = Color.alphaBlend(Colors.black.withValues(alpha: 0.54), Colors.white);
      expect(contrast(Colors.white, worst), greaterThanOrEqualTo(4.5));
    });
  });
}
