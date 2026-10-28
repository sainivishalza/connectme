import 'package:flutter/material.dart';

import '../../../core/theme/tokens.dart';
import '../../../shared/widgets/app_button.dart';

class TierInfo {
  const TierInfo(this.id, this.name, this.price, this.perks);
  final String id;
  final String name;
  final String price;
  final List<String> perks;
}

/// Display copy for the plans. Prices here are labels only: the amount that is
/// charged is defined by the server.
const tiers = [
  TierInfo('silver', 'Silver', '\$9.99/mo', ['Unlimited likes', '5 Super Likes a day', 'No ads']),
  TierInfo('gold', 'Gold', '\$19.99/mo',
      ['Everything in Silver', 'See who liked you', '1 free boost a week', 'Invisible mode']),
  TierInfo('platinum', 'Platinum', '\$39.99/mo',
      ['Everything in Gold', 'Priority matching', 'Unlimited boosts', 'VIP badge']),
];

class TierCard extends StatelessWidget {
  const TierCard({super.key, required this.info, required this.current, required this.onSelect, this.busy = false});

  final TierInfo info;
  final bool current;
  final VoidCallback? onSelect;
  final bool busy;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final cs = t.colorScheme;
    // Each plan has its own light: Gold glows warm and is marked as the
    // popular pick, Platinum takes the Incognito violet, Silver stays plain.
    final light = switch (info.id) {
      'gold' => ModeLight.lamplight,
      'platinum' => ModeLight.dusk,
      _ => null,
    };
    final popular = info.id == 'gold';
    final accent = light?.solid ?? cs.onSurfaceVariant;
    final border = current ? cs.primary : (light?.start.withValues(alpha: 0.7) ?? cs.outlineVariant);
    return Container(
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(Radii.card),
        gradient: light?.wash(t.brightness),
        color: light == null ? cs.surfaceContainerLow : null,
        border: Border.all(color: border, width: current || popular ? 2 : 1.2),
      ),
      child: Padding(
        padding: const EdgeInsets.all(18),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              crossAxisAlignment: CrossAxisAlignment.center,
              children: [
                Text(info.name, style: t.textTheme.titleLarge),
                if (popular) ...[
                  const SizedBox(width: 10),
                  Container(
                    padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 3),
                    decoration: BoxDecoration(gradient: ModeLight.lamplight.gradient, borderRadius: BorderRadius.circular(Radii.pill)),
                    child: Text('Most popular', style: t.textTheme.labelSmall?.copyWith(color: ModeLight.lamplight.ink, fontWeight: FontWeight.w800)),
                  ),
                ],
                const Spacer(),
                Text(info.price, style: t.textTheme.titleMedium),
              ],
            ),
            const SizedBox(height: 10),
            for (final p in info.perks)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 3),
                child: Row(children: [Icon(Icons.check_circle, size: 18, color: accent), const SizedBox(width: 10), Expanded(child: Text(p))]),
              ),
            const SizedBox(height: 14),
            AppButton(
              key: Key('tier_${info.id}'),
              label: current ? 'Current plan' : 'Choose ${info.name}',
              busy: busy,
              onPressed: current ? null : onSelect,
            ),
          ],
        ),
      ),
    );
  }
}
