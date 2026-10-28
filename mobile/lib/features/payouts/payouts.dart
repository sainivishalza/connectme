import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models/json.dart';
import '../../core/network/api_client.dart';
import '../../core/providers.dart';
import '../../shared/widgets/app_snack.dart';
import '../../shared/widgets/states.dart';

class PayoutLine {
  const PayoutLine({required this.id, required this.coins, required this.status, this.reference = '', this.note = '', this.amountCents = 0});
  final String id;
  final int coins;
  final String status;
  final String reference;
  final String note;
  final int amountCents;

  factory PayoutLine.fromJson(Map<String, dynamic> j) => PayoutLine(
    id: jsonString(j['id']),
    coins: jsonInt(j['coins']),
    status: jsonString(j['status']),
    reference: jsonString(j['reference']),
    note: jsonString(j['note']),
    amountCents: jsonInt(j['amount_cents']),
  );
}

class Earnings {
  const Earnings({
    this.available = 0,
    this.requested = 0,
    this.paid = 0,
    this.minPayout = 5000,
    this.canRequest = false,
    this.needsId = '',
    this.payouts = const [],
  });
  final int available;
  final int requested;
  final int paid;
  final int minPayout;
  final bool canRequest;
  final String needsId;
  final List<PayoutLine> payouts;

  factory Earnings.fromJson(Map<String, dynamic> j) => Earnings(
    available: jsonInt(j['available_coins']),
    requested: jsonInt(j['requested_coins']),
    paid: jsonInt(j['paid_coins']),
    minPayout: j['min_payout_coins'] is num ? jsonInt(j['min_payout_coins']) : 5000,
    canRequest: jsonBool(j['can_request']),
    needsId: jsonString(j['needs_id']),
    payouts: listOf(j['payouts'], PayoutLine.fromJson),
  );
}

class PayoutsRepository {
  PayoutsRepository(this._api);
  final ApiClient _api;
  Future<Earnings> mine() async => Earnings.fromJson(await _api.getMap('/payouts/mine'));
  Future<void> request() => _api.post('/payouts/request');
}

final payoutsRepositoryProvider = Provider<PayoutsRepository>((ref) => PayoutsRepository(ref.watch(apiClientProvider)));

final earningsProvider = FutureProvider.autoDispose<Earnings>((ref) => ref.watch(payoutsRepositoryProvider).mine());

String _dollars(int cents) => '\$${(cents / 100).toStringAsFixed(2)}';

/// What a host has earned from live gifts and how to be paid. The app does not
/// move money: a request goes to the team, who send it and record the transfer.
class EarningsScreen extends ConsumerWidget {
  const EarningsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final t = Theme.of(context);
    final v = ref.watch(earningsProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Earnings and payouts')),
      body: v.when(
        loading: () => const Loader(),
        error: (e, _) => ErrorView(error: e, onRetry: () => ref.invalidate(earningsProvider)),
        data: (e) => ListView(
          padding: const EdgeInsets.all(16),
          children: [
            Card(
              child: Padding(
                padding: const EdgeInsets.all(16),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text('Ready to be paid', style: t.textTheme.labelLarge),
                    Text('${e.available} coins', key: const Key('available_coins'), style: t.textTheme.headlineMedium),
                    const SizedBox(height: 8),
                    if (e.requested > 0) Text('${e.requested} coins are in a payout request the team has not paid yet.', key: const Key('requested_note'), style: t.textTheme.bodyMedium),
                    if (e.paid > 0) Text('${e.paid} coins paid out so far.', style: t.textTheme.bodyMedium),
                  ],
                ),
              ),
            ),
            const SizedBox(height: 12),
            if (e.needsId.isNotEmpty) Text(e.needsId, key: const Key('needs_id'), style: t.textTheme.bodyMedium),
            if (e.needsId.isEmpty && e.requested == 0 && e.available < e.minPayout)
              Text('You can ask for a payout from ${e.minPayout} coins.', key: const Key('below_min'), style: t.textTheme.bodyMedium),
            const SizedBox(height: 8),
            FilledButton(
              key: const Key('request_payout'),
              onPressed: e.canRequest
                  ? () async {
                      final ok = await guarded(context, () async {
                        await ref.read(payoutsRepositoryProvider).request();
                        ref.invalidate(earningsProvider);
                      });
                      if (ok && context.mounted) showMessage(context, 'Payout requested. The team will send the money and record it here.');
                    }
                  : null,
              child: const Text('Ask for a payout'),
            ),
            const SizedBox(height: 20),
            Text('History', style: t.textTheme.titleMedium),
            if (e.payouts.isEmpty) const Padding(padding: EdgeInsets.only(top: 8), child: Text('No payouts yet.')),
            for (final p in e.payouts)
              Card(
                child: ListTile(
                  key: Key('payout_${p.id}'),
                  title: Text('${p.coins} coins${p.status == 'paid' ? ' - ${_dollars(p.amountCents)}' : ''}'),
                  subtitle: Text(
                    switch (p.status) {
                      'paid' => 'Paid. Reference ${p.reference}',
                      'rejected' => 'Not paid: ${p.note.isEmpty ? 'see the team' : p.note}',
                      _ => 'Waiting for the team',
                    },
                  ),
                ),
              ),
          ],
        ),
      ),
    );
  }
}
