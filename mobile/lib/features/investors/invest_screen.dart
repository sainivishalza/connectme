import 'package:flutter/material.dart';
import '../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../shared/widgets/app_snack.dart';
import '../../shared/widgets/states.dart';
import '../auth/auth_controller.dart';
import 'investors_repository.dart';

const _common = 'This page records pledges. Nothing is sold or issued until a written agreement is signed with you. ';
const _risk = 'A start-up can lose all the money put into it. This is not financial advice.';

/// The notice for the model in use. Dilution is the unusual part of the
/// pro-rata model, so it is said plainly; the fixed model says it sells out.
String noticeFor(OwnershipBoard b) => b.fixed
    ? '${_common}51% stays with the founder. The other 49% is sold at a fixed price of \$${b.pricePerPercent} for 1%, '
        'so what you buy keeps its percentage; when the 49% is sold, it is sold out. $_risk'
    : '${_common}51% stays with the founder; the other 49% is shared between investors in proportion to the money confirmed, '
        'so every new investment lowers everyone else\'s percentage. Starting price: \$${b.pricePerPercent} for 1% of the company. $_risk';

/// What one dollar buys, as a percentage, at [price] dollars for 1%.
String _oneDollar(String price) {
  final p = double.tryParse(price);
  if (p == null || p <= 0) return '0.001';
  final f = (1 / p).toStringAsFixed(6).replaceFirst(RegExp(r'0+$'), '');
  return f.endsWith('.') ? f.substring(0, f.length - 1) : f;
}

/// Public page: who owns ConnectMe, and a way to pledge. The board shows only
/// names and percentages. Reading it needs no account; pledging does.
class InvestScreen extends ConsumerStatefulWidget {
  const InvestScreen({super.key});

  @override
  ConsumerState<InvestScreen> createState() => _InvestScreenState();
}

class _InvestScreenState extends ConsumerState<InvestScreen> {
  final _amount = TextEditingController();
  bool _showName = true;
  bool _accepted = false;
  bool _busy = false;

  @override
  void dispose() {
    _amount.dispose();
    super.dispose();
  }

  bool get _valid => RegExp(r'^\d{1,6}(\.\d{1,2})?$').hasMatch(_amount.text.trim()) && (double.tryParse(_amount.text.trim()) ?? 0) >= 1;

  Future<void> _pledge() async {
    setState(() => _busy = true);
    final ok = await guarded(context, () async {
      await ref.read(investorsRepositoryProvider).pledge(_amount.text.trim(), showName: _showName);
      ref.invalidate(myInvestmentsProvider);
    });
    if (!mounted) return;
    setState(() => _busy = false);
    if (ok) {
      _amount.clear();
      showMessage(context, 'Pledge recorded. Send the money as described below and it will be confirmed.');
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final board = ref.watch(ownershipBoardProvider);
    final signedIn = ref.watch(authControllerProvider.select((s) => s.status)) == AuthStatus.signedIn;
    return Scaffold(
      appBar: AppBar(title: const Text('Own a part of ConnectMe')),
      body: board.when(
        loading: () => const Loader(),
        error: (e, _) => ErrorView(error: e, onRetry: () => ref.invalidate(ownershipBoardProvider)),
        data: (b) => ListView(padding: const EdgeInsets.all(16), children: [
          Card(
            key: const Key('invest_notice'),
            color: t.colorScheme.secondaryContainer,
            child: Padding(padding: const EdgeInsets.all(14), child: Text(noticeFor(b), style: t.textTheme.bodySmall)),
          ),
          const SizedBox(height: 16),
          Text('Who owns ConnectMe', style: t.textTheme.titleLarge),
          const SizedBox(height: 8),
          for (final h in b.holders)
            Card(
              child: ListTile(
                key: Key('holder_${h.rank}'),
                leading: CircleAvatar(child: h.founder ? const Icon(Icons.workspace_premium) : Text('${h.rank}')),
                title: Text(h.name, maxLines: 1, overflow: TextOverflow.ellipsis),
                subtitle: h.founder ? const Text('Founder') : null,
                trailing: Text('${h.percent}%', key: Key('holder_pct_${h.rank}'), style: t.textTheme.titleMedium),
              ),
            ),
          if (b.holders.length == 1)
            Padding(
              padding: const EdgeInsets.symmetric(vertical: 12),
              child: Text(b.fixed ? '${b.openPercent}% is open at \$${b.pricePerPercent} for 1%. Be the first.' : '${b.poolPercent}% is open to investors. Be the first.', style: t.textTheme.bodyMedium),
            ),
          const SizedBox(height: 20),
          if (!signedIn)
            FilledButton(key: const Key('invest_signin'), onPressed: () => context.go('/login'), child: Text(AppLocalizations.of(context)?.sSignInToPledge ?? 'Sign in to pledge'))
          else ...[
            Text('Pledge an amount', style: t.textTheme.titleLarge),
            const SizedBox(height: 8),
            TextField(
              key: const Key('invest_amount'),
              controller: _amount,
              keyboardType: const TextInputType.numberWithOptions(decimal: true),
              decoration: InputDecoration(labelText: 'Amount in US dollars', prefixText: '\$ ', helperText: 'From \$1. \$1 is ${_oneDollar(b.pricePerPercent)}% at the ${b.fixed ? 'fixed' : 'starting'} price.'),
              onChanged: (_) => setState(() {}),
            ),
            SwitchListTile(
              key: const Key('invest_show_name'),
              contentPadding: EdgeInsets.zero,
              title: const Text('Show my name on the board'),
              subtitle: const Text('Otherwise you appear as "Anonymous investor"'),
              value: _showName,
              onChanged: (v) => setState(() => _showName = v),
            ),
            CheckboxListTile(
              key: const Key('invest_accept'),
              contentPadding: EdgeInsets.zero,
              controlAffinity: ListTileControlAffinity.leading,
              title: const Text('I have read the notice above'),
              value: _accepted,
              onChanged: (v) => setState(() => _accepted = v ?? false),
            ),
            FilledButton(key: const Key('invest_submit'), onPressed: _valid && _accepted && !_busy ? _pledge : null, child: const Text('Record my pledge')),
            const SizedBox(height: 20),
            const _MyPledges(),
          ],
        ]),
      ),
    );
  }
}

class _MyPledges extends ConsumerWidget {
  const _MyPledges();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final t = Theme.of(context);
    final mine = ref.watch(myInvestmentsProvider);
    return mine.when(
      loading: () => const SizedBox.shrink(),
      error: (e, _) => const SizedBox.shrink(),
      data: (m) => m.pledges.isEmpty
          ? const SizedBox.shrink()
          : Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text('Your pledges', style: t.textTheme.titleLarge),
              const SizedBox(height: 4),
              Text('You own ${m.percent}% today', key: const Key('my_percent'), style: t.textTheme.bodyMedium),
              if (m.paymentNote.isNotEmpty) ...[const SizedBox(height: 6), Text(m.paymentNote, key: const Key('payment_note'), style: t.textTheme.bodySmall)],
              const SizedBox(height: 8),
              for (final p in m.pledges)
                Card(
                  child: ListTile(
                    key: Key('pledge_${p.id}'),
                    title: Text('\$${p.amount}'),
                    subtitle: Text('${p.status} - ${p.reference}'),
                    trailing: p.status == 'pending'
                        ? TextButton(
                            key: Key('cancel_${p.id}'),
                            onPressed: () async {
                              final ok = await guarded(context, () => ref.read(investorsRepositoryProvider).cancel(p.id));
                              if (ok) ref.invalidate(myInvestmentsProvider);
                            },
                            child: Text(AppLocalizations.of(context)?.sCancel ?? 'Cancel'),
                          )
                        : null,
                  ),
                ),
            ]),
    );
  }
}
