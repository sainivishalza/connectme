import 'package:flutter/material.dart';

/// The looks a live room can have. The host picks one; viewers see it as the
/// tint behind the room and as the colour of its card in the list. It is
/// decoration only and never changes who can see or do what.
class RoomTheme {
  const RoomTheme(this.id, this.label, this.top, this.bottom);
  final String id;
  final String label;
  final Color top;
  final Color bottom;

  LinearGradient get gradient => LinearGradient(begin: Alignment.topCenter, end: Alignment.bottomCenter, colors: [top, bottom]);

  static const plain = RoomTheme('plain', 'Plain', Colors.transparent, Colors.transparent);
  static const all = [
    plain,
    RoomTheme('sunset', 'Sunset', Color(0x00FF8A5C), Color(0xB3C2306B)),
    RoomTheme('neon', 'Neon', Color(0x007C4DFF), Color(0xB300B8D4)),
    RoomTheme('forest', 'Forest', Color(0x0030A46C), Color(0xB30B3D2E)),
    RoomTheme('ocean', 'Ocean', Color(0x0029B6F6), Color(0xB30D2B6B)),
    RoomTheme('velvet', 'Velvet', Color(0x00B3446C), Color(0xB32A0A2E)),
  ];

  static RoomTheme of(String id) => all.firstWhere((t) => t.id == id, orElse: () => plain);

  /// A solid colour for list cards and chips.
  Color get accent => id == 'plain' ? Colors.grey : bottom.withValues(alpha: 1);
}

/// Paints a room's tint over its video. Plain rooms get nothing.
class RoomTint extends StatelessWidget {
  const RoomTint({super.key, required this.theme});
  final RoomTheme theme;

  @override
  Widget build(BuildContext context) {
    if (theme.id == 'plain') return const SizedBox.shrink();
    return IgnorePointer(child: DecoratedBox(key: Key('room_tint_${theme.id}'), decoration: BoxDecoration(gradient: theme.gradient), child: const SizedBox.expand()));
  }
}
