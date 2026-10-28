import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/models.dart';
import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../groups_repository.dart';

/// Dates proposed for a matched pair of groups.
class GroupDatesScreen extends ConsumerWidget {
  const GroupDatesScreen({super.key, required this.groupId, required this.matchId});
  final String groupId;
  final String matchId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final dates = ref.watch(groupDatesProvider(matchId));
    return Scaffold(
      appBar: AppBar(title: const Text('Plan your date')),
      floatingActionButton: FloatingActionButton.extended(
        key: const Key('propose_date'),
        onPressed: () => _propose(context, ref),
        icon: const Icon(Icons.event),
        label: Text(AppLocalizations.of(context)?.sPropose ?? 'Propose'),
      ),
      body: AsyncListView<DateView>(
        value: dates,
        onRefresh: () async => ref.refresh(groupDatesProvider(matchId).future),
        emptyTitle: 'No dates planned',
        emptyMessage: 'Propose a time and place. It is confirmed once someone from each group agrees.',
        emptyIcon: Icons.event_available_outlined,
        itemBuilder: (c, d) => Card(
          child: ListTile(
            leading: Icon(d.status == 'confirmed' ? Icons.event_available : Icons.event_outlined),
            title: Text(d.venue),
            subtitle: Text('${d.startsAt.toString().split('.').first}${d.address.isEmpty ? '' : '\n${d.address}'}'),
            isThreeLine: d.address.isNotEmpty,
            trailing: d.status == 'confirmed'
                ? const Chip(label: Text('Confirmed'))
                : (d.iConfirmed
                    ? const Chip(label: Text('Waiting'))
                    : FilledButton(
                        key: Key('confirm_${d.id}'),
                        onPressed: () async {
                          final ok = await guarded(c, () => ref.read(groupsRepositoryProvider).confirmDate(d.id));
                          if (ok) ref.invalidate(groupDatesProvider(matchId));
                        },
                        style: FilledButton.styleFrom(minimumSize: const Size(80, 40)),
                        child: Text(AppLocalizations.of(context)?.sConfirm ?? 'Confirm'),
                      )),
            onTap: d.status == 'cancelled' ? null : () => _logistics(c, ref, d),
            onLongPress: () async {
              final ok = await guarded(c, () => ref.read(groupsRepositoryProvider).cancelDate(d.id));
              if (ok) ref.invalidate(groupDatesProvider(matchId));
            },
          ),
        ),
      ),
    );
  }

  Future<void> _logistics(BuildContext context, WidgetRef ref, DateView d) async {
    final repo = ref.read(groupsRepositoryProvider);
    Logistics? l;
    if (!await guarded(context, () async => l = await repo.logistics(d.id)) || l == null || !context.mounted) return;
    if (!l!.paid) {
      final pay = await showDialog<bool>(
        context: context,
        builder: (c) => AlertDialog(
          title: const Text('Plan the day'),
          content: Text('Venue ideas, icebreakers and a short plan for ${d.venue}, ready for both groups. ${l!.priceCoins} coins, once for you.'),
          actions: [
            TextButton(onPressed: () => Navigator.pop(c, false), child: Text(AppLocalizations.of(context)?.sNotNow ?? 'Not now')),
            FilledButton(key: const Key('pay_logistics'), onPressed: () => Navigator.pop(c, true), child: Text('Pay ${l!.priceCoins} coins')),
          ],
        ),
      );
      if (pay != true || !context.mounted) return;
      if (!await guarded(context, () async => l = await repo.payLogistics(d.id)) || l == null || !context.mounted) return;
    }
    final plan = l!.plan;
    if (plan == null) return;
    await showModalBottomSheet<void>(
      context: context, useRootNavigator: true,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (c) => SafeArea(
        child: ListView(shrinkWrap: true, padding: const EdgeInsets.fromLTRB(20, 0, 20, 20), children: [
          Text('Plan for ${d.venue}', style: Theme.of(c).textTheme.titleLarge),
          for (final part in [('Plan', plan.plan), ('Venue ideas', plan.venues), ('Icebreakers', plan.icebreakers)])
            if (part.$2.isNotEmpty) ...[
              const SizedBox(height: 16),
              Text(part.$1, style: Theme.of(c).textTheme.titleSmall),
              for (final line in part.$2) Padding(padding: const EdgeInsets.only(top: 6), child: Text(line)),
            ],
        ]),
      ),
    );
  }

  Future<void> _propose(BuildContext context, WidgetRef ref) async {
    final venue = TextEditingController();
    final address = TextEditingController();
    var when = DateTime.now().add(const Duration(days: 2));
    final go = await showDialog<bool>(
      context: context,
      builder: (c) => StatefulBuilder(
        builder: (c, set) => AlertDialog(
          title: Text(AppLocalizations.of(context)?.sProposeADate ?? 'Propose a date'),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              TextField(key: const Key('venue'), controller: venue, decoration: const InputDecoration(labelText: 'Venue')),
              TextField(controller: address, decoration: const InputDecoration(labelText: 'Address (optional)')),
              const SizedBox(height: 12),
              OutlinedButton.icon(
                icon: const Icon(Icons.schedule),
                label: Text(when.toString().split('.').first),
                onPressed: () async {
                  final d = await showDatePicker(
                      context: c, initialDate: when, firstDate: DateTime.now(), lastDate: DateTime.now().add(const Duration(days: 89)));
                  if (d == null || !c.mounted) return;
                  final t = await showTimePicker(context: c, initialTime: TimeOfDay.fromDateTime(when));
                  if (t != null) set(() => when = DateTime(d.year, d.month, d.day, t.hour, t.minute));
                },
              ),
            ],
          ),
          actions: [
            TextButton(onPressed: () => Navigator.pop(c, false), child: Text(AppLocalizations.of(context)?.sCancel ?? 'Cancel')),
            FilledButton(onPressed: () => Navigator.pop(c, true), child: Text(AppLocalizations.of(context)?.sPropose ?? 'Propose')),
          ],
        ),
      ),
    );
    if (go != true || !context.mounted || venue.text.trim().isEmpty) return;
    final ok = await guarded(
      context,
      () => ref.read(groupsRepositoryProvider).schedule(groupId, groupMatchId: matchId, venue: venue.text.trim(), address: address.text.trim(), startsAt: when),
    );
    if (ok) ref.invalidate(groupDatesProvider(matchId));
  }
}
