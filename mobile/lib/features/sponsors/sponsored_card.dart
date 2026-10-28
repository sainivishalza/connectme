import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../shared/widgets/app_snack.dart';
import '../payments/payments_repository.dart' show urlOpenerProvider;
import 'sponsors_repository.dart';

/// One labelled brand placement at the top of a feed. Shows nothing when the
/// member has no placement (Silver and above, recently shown, or none nearby)
/// and never blocks the feed if the request fails.
class SponsoredSlot extends ConsumerStatefulWidget {
  const SponsoredSlot({super.key});

  @override
  ConsumerState<SponsoredSlot> createState() => _SponsoredSlotState();
}

class _SponsoredSlotState extends ConsumerState<SponsoredSlot> {
  Sponsored? _ad;

  @override
  void initState() {
    super.initState();
    _fetch();
  }

  Future<void> _fetch() async {
    try {
      final ad = await ref.read(sponsorsRepositoryProvider).feed();
      if (mounted && ad != null) setState(() => _ad = ad);
    } catch (_) {
      // An ad must never get in the way of the feed.
    }
  }

  Future<void> _open(Sponsored ad) async {
    await guarded(context, () async {
      final link = await ref.read(sponsorsRepositoryProvider).click(ad.id);
      if (link.isNotEmpty) await ref.read(urlOpenerProvider).open(link);
    });
  }

  @override
  Widget build(BuildContext context) {
    final ad = _ad;
    if (ad == null) return const SizedBox.shrink();
    final theme = Theme.of(context);
    return Card(
      key: const Key('sponsored_card'),
      child: InkWell(
        onTap: ad.linkUrl.isEmpty ? null : () => _open(ad),
        borderRadius: BorderRadius.circular(16),
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Text('Sponsored · ${ad.advertiser}', style: theme.textTheme.labelMedium),
            const SizedBox(height: 6),
            Text(ad.headline, style: theme.textTheme.titleMedium),
            if (ad.body.isNotEmpty) ...[const SizedBox(height: 4), Text(ad.body)],
            if (ad.linkUrl.isNotEmpty) ...[
              const SizedBox(height: 8),
              Text('Learn more', style: theme.textTheme.labelLarge?.copyWith(color: theme.colorScheme.primary)),
            ],
          ]),
        ),
      ),
    );
  }
}
