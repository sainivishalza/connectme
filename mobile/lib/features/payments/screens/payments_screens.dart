import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/models.dart';
import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../payments_repository.dart';
import '../widgets/tier_card.dart';

/// Subscription tiers.
class SubscriptionScreen extends ConsumerStatefulWidget {
  const SubscriptionScreen({super.key});
  @override
  ConsumerState<SubscriptionScreen> createState() => _SubscriptionScreenState();
}

class _SubscriptionScreenState extends ConsumerState<SubscriptionScreen> {
  String? _busy;

  Future<void> _choose(String tier) async {
    setState(() => _busy = tier);
    await guarded(context, () async {
      final url = await ref.read(paymentsRepositoryProvider).subscribe(tier);
      final opened = await ref.read(urlOpenerProvider).open(url);
      if (!opened && mounted) showMessage(context, 'Could not open the checkout page.');
    });
    if (mounted) setState(() => _busy = null);
  }

  Future<void> _cancel() async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (c) => AlertDialog(
        title: const Text('Cancel subscription?'),
        content: const Text('You keep your perks until the end of the billing period.'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(c, false), child: const Text('Keep')),
          TextButton(onPressed: () => Navigator.pop(c, true), child: const Text('Cancel plan')),
        ],
      ),
    );
    if (ok != true || !mounted) return;
    final done = await guarded(context, () => ref.read(paymentsRepositoryProvider).cancel());
    if (done && mounted) {
      showMessage(context, 'Cancellation requested');
      ref.invalidate(accountProvider);
    }
  }

  @override
  Widget build(BuildContext context) {
    final acct = ref.watch(accountProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Subscription')),
      body: acct.when(
        loading: () => const Loader(),
        error: (e, _) => ErrorView(error: e, onRetry: () => ref.invalidate(accountProvider)),
        data: (a) => RefreshIndicator(
          onRefresh: () async => ref.refresh(accountProvider.future),
          child: ListView(
            padding: const EdgeInsets.all(16),
            children: [
              if (a.tier.isNotEmpty && a.endsAt != null)
                Padding(
                  padding: const EdgeInsets.only(bottom: 12),
                  child: Text(a.cancelsAtEnd
                      ? 'Ends on ${a.endsAt!.toLocal().toString().split(' ').first}'
                      : 'Renews on ${a.endsAt!.toLocal().toString().split(' ').first}'),
                ),
              for (final t in tiers)
                Padding(
                  padding: const EdgeInsets.only(bottom: 12),
                  child: TierCard(
                    info: t,
                    current: a.tier == t.id,
                    busy: _busy == t.id,
                    // Switching plans needs a cancel first (the server refuses a second subscription).
                    onSelect: a.tier.isNotEmpty ? null : () => _choose(t.id),
                  ),
                ),
              if (a.tier.isNotEmpty && !a.cancelsAtEnd)
                TextButton(onPressed: _cancel, child: const Text('Cancel subscription')),
            ],
          ),
        ),
      ),
    );
  }
}

/// Coin shop.
class CoinShopScreen extends ConsumerWidget {
  const CoinShopScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final acct = ref.watch(accountProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Coins')),
      body: acct.when(
        loading: () => const Loader(),
        error: (e, _) => ErrorView(error: e, onRetry: () => ref.invalidate(accountProvider)),
        data: (a) => RefreshIndicator(
          onRefresh: () async => ref.refresh(accountProvider.future),
          child: ListView(
            padding: const EdgeInsets.all(16),
            children: [
              Card(
                child: ListTile(
                  leading: const Icon(Icons.monetization_on),
                  title: Text('${a.coins} coins'),
                  subtitle: const Text('Use coins for Super Likes, boosts and gifts'),
                ),
              ),
              const SizedBox(height: 12),
              for (final p in a.packs)
                Card(
                  child: ListTile(
                    key: Key('pack_${p.id}'),
                    title: Text('${p.coins} coins'),
                    trailing: FilledButton(
                      onPressed: () => _buy(context, ref, p),
                      style: FilledButton.styleFrom(minimumSize: const Size(80, 40)),
                      child: Text(p.price),
                    ),
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }

  Future<void> _buy(BuildContext context, WidgetRef ref, CoinPack p) async {
    await guarded(context, () async {
      final url = await ref.read(paymentsRepositoryProvider).buyCoins(p.id);
      await ref.read(urlOpenerProvider).open(url);
    });
  }
}

/// Gifts received.
class GiftsScreen extends ConsumerWidget {
  const GiftsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final gifts = ref.watch(receivedGiftsProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Gifts received')),
      body: AsyncListView<ReceivedGift>(
        value: gifts,
        onRefresh: () async => ref.refresh(receivedGiftsProvider.future),
        emptyTitle: 'No gifts yet',
        emptyMessage: 'Gifts from your matches show up here.',
        emptyIcon: Icons.card_giftcard,
        itemBuilder: (_, g) => Card(
          child: ListTile(
            leading: const Icon(Icons.card_giftcard),
            title: Text(g.name),
            subtitle: Text('From someone special on ${g.createdAt.toLocal().toString().split('.').first}'),
          ),
        ),
      ),
    );
  }
}

/// Bottom sheet to choose a gift for a person (used from chat).
Future<void> showGiftShop(BuildContext context, WidgetRef ref, {required String toRef}) {
  return showModalBottomSheet<void>(
    context: context,
    showDragHandle: true,
    builder: (c) => Consumer(
      builder: (c, ref, _) {
        final cat = ref.watch(giftCatalogProvider);
        return cat.when(
          loading: () => const SizedBox(height: 200, child: Loader()),
          error: (e, _) => SizedBox(height: 200, child: ErrorView(error: e, onRetry: () => ref.invalidate(giftCatalogProvider))),
          data: (gifts) => ListView(
            shrinkWrap: true,
            children: [
              for (final g in gifts)
                ListTile(
                  key: Key('gift_${g.id}'),
                  leading: const Icon(Icons.card_giftcard),
                  title: Text(g.name),
                  trailing: Text('${g.coins} coins'),
                  onTap: () async {
                    final nav = Navigator.of(c);
                    final ok = await guarded(c, () => ref.read(paymentsRepositoryProvider).sendGift(
                          toRef: toRef,
                          giftId: g.id,
                          requestId: '${DateTime.now().microsecondsSinceEpoch}-${g.id}',
                        ));
                    if (ok) {
                      ref.invalidate(accountProvider);
                      nav.pop();
                    }
                  },
                ),
            ],
          ),
        );
      },
    ),
  );
}
