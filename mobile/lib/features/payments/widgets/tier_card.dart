import 'package:flutter/material.dart';

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
    return Card(
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(16),
        side: current ? BorderSide(color: t.colorScheme.primary, width: 2) : BorderSide.none,
      ),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Text(info.name, style: t.textTheme.titleLarge),
                const Spacer(),
                Text(info.price, style: t.textTheme.titleMedium),
              ],
            ),
            const SizedBox(height: 8),
            for (final p in info.perks)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 2),
                child: Row(children: [const Icon(Icons.check, size: 18), const SizedBox(width: 8), Expanded(child: Text(p))]),
              ),
            const SizedBox(height: 12),
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
