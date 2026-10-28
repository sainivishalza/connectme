import 'package:flutter/material.dart';

/// Design tokens for "the hour you choose to be seen".
///
/// The interface is quiet and neutral; colour with meaning belongs to the
/// visibility modes. Each mode is a time of day with its own light, and that
/// light appears only where it carries information (the mode panel, avatar
/// rings, the glow behind anonymous people), never as general decoration.
class Palette {
  Palette._();

  static const midnight = Color(0xFF17172E); // text, primary buttons, nav
  static const porcelain = Color(0xFFF4F5F8); // light surface (cool, not cream)
  static const paper = Color(0xFFFFFFFF);
  static const mist = Color(0xFFE6E8EF); // quiet fills and hairlines
  static const slate = Color(0xFF5E6075); // secondary text (AA on porcelain)

  // Dark theme surfaces.
  static const night = Color(0xFF101022);
  static const nightRaised = Color(0xFF1B1B34);
  static const nightLine = Color(0xFF2C2C4A);
  static const nightText = Color(0xFFE9EAF3);
  static const nightMuted = Color(0xFFA3A5BD);

  static const danger = Color(0xFFC9304F);
  static const dangerOnDark = Color(0xFFFF6B88); // the red above is too dim on the dark surfaces
}

/// The light of one visibility mode.
@immutable
class ModeLight {
  const ModeLight._(this.id, this.label, this.start, this.end, this.ink, this.solid);

  final String id;
  final String label;
  final Color start;
  final Color end;
  final Color ink; // readable text/icon colour on the gradient
  final Color solid; // flat stand-in where a gradient is not possible

  LinearGradient get gradient => LinearGradient(
        begin: Alignment.topLeft,
        end: Alignment.bottomRight,
        colors: [start, end],
      );

  /// A soft wash of this light for large quiet areas.
  LinearGradient wash(Brightness b) => LinearGradient(
        begin: Alignment.topLeft,
        end: Alignment.bottomRight,
        colors: [
          start.withValues(alpha: b == Brightness.light ? 0.22 : 0.28),
          end.withValues(alpha: b == Brightness.light ? 0.10 : 0.16),
        ],
      );

  static const daylight = ModeLight._('open', 'Open', Color(0xFFFFA77F), Color(0xFFEC5E8E), Color(0xFF3A0F22), Color(0xFFD63B72));
  static const dusk = ModeLight._('incognito', 'Incognito', Color(0xFF2E2A6E), Color(0xFF6A58E0), Color(0xFFFFFFFF), Color(0xFF5B4FD0));
  static const lamplight = ModeLight._('hotel', 'Hotel', Color(0xFFF4D68A), Color(0xFFC9822B), Color(0xFF3A2400), Color(0xFF8A5A12));

  static ModeLight of(String mode) => switch (mode) {
        'incognito' => dusk,
        'hotel' => lamplight,
        _ => daylight,
      };
}

/// Radii follow importance: hero surfaces are rounder than cards, cards
/// rounder than controls, so shape tells you what you are touching.
class Radii {
  Radii._();
  static const hero = 28.0;
  static const card = 20.0;
  static const control = 14.0;
  static const pill = 999.0;
}

class Fonts {
  Fonts._();
  static const display = 'Gloock';
  static const ui = 'Figtree';
}
