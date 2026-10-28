import 'package:flutter/material.dart';

import '../../../core/theme/tokens.dart';

/// The mode panel: the one place where the whole interface changes colour.
///
/// Open / Incognito selector with a plain-language explanation of each. In
/// hotel mode the choice is limited: incognito is forced, open is an explicit
/// opt-in. The panel wears the light of the current mode, so how visible you
/// are is something you can see, not something you have to read.
class ModeSwitch extends StatelessWidget {
  const ModeSwitch({super.key, required this.mode, required this.onChanged, this.busy = false});

  final String mode; // open | incognito | hotel
  final ValueChanged<String> onChanged;
  final bool busy;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final light = ModeLight.of(mode);
    final hotel = mode == 'hotel';
    final selected = hotel ? 'incognito' : mode;
    final headline = switch (mode) {
      'open' => 'Seen in full',
      'incognito' => 'Seen from a distance',
      'hotel' => 'Anonymous at this venue',
      _ => '',
    };
    final explain = switch (mode) {
      'open' => 'People see your name, photos and bio.',
      'incognito' => 'People see only your tags, intro and rough distance. Photos unlock only if you both reveal.',
      'hotel' => 'You are checked in at a venue: anonymous until you both reveal. Choose Open to opt out.',
      _ => '',
    };
    final icon = switch (mode) {
      'incognito' => Icons.nights_stay_outlined,
      'hotel' => Icons.hotel_outlined,
      _ => Icons.wb_sunny_outlined,
    };
    final still = MediaQuery.disableAnimationsOf(context);
    return AnimatedContainer(
      duration: still ? Duration.zero : const Duration(milliseconds: 420),
      curve: Curves.easeOutCubic,
      padding: const EdgeInsets.fromLTRB(20, 20, 20, 18),
      decoration: BoxDecoration(
        gradient: light.wash(t.brightness),
        borderRadius: BorderRadius.circular(Radii.hero),
        border: Border.all(color: light.solid.withValues(alpha: 0.28)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              AnimatedContainer(
                duration: still ? Duration.zero : const Duration(milliseconds: 420),
                width: 44,
                height: 44,
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  gradient: light.gradient,
                  boxShadow: [BoxShadow(color: light.solid.withValues(alpha: 0.45), blurRadius: 18, offset: const Offset(0, 6))],
                ),
                child: Icon(icon, color: light.ink, size: 22),
              ),
              const SizedBox(width: 14),
              Expanded(child: Text(headline, style: t.textTheme.headlineSmall)),
            ],
          ),
          const SizedBox(height: 12),
          Text(explain, style: t.textTheme.bodyMedium),
          const SizedBox(height: 16),
          SegmentedButton<String>(
            key: const Key('mode_switch'),
            showSelectedIcon: false,
            expandedInsets: EdgeInsets.zero,
            style: SegmentedButton.styleFrom(
              backgroundColor: t.colorScheme.surface.withValues(alpha: 0.7),
              selectedBackgroundColor: light.solid,
              selectedForegroundColor: Colors.white,
              foregroundColor: t.colorScheme.onSurface,
            ),
            segments: const [
              ButtonSegment(value: 'open', label: Text('Open'), icon: Icon(Icons.wb_sunny_outlined)),
              ButtonSegment(value: 'incognito', label: Text('Incognito'), icon: Icon(Icons.nights_stay_outlined)),
            ],
            selected: {selected},
            onSelectionChanged: busy ? null : (s) => onChanged(s.first),
          ),
        ],
      ),
    );
  }
}
