import 'package:flutter/material.dart';
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
        label: const Text('Propose'),
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
                        child: const Text('Confirm'),
                      )),
            onLongPress: () async {
              final ok = await guarded(c, () => ref.read(groupsRepositoryProvider).cancelDate(d.id));
              if (ok) ref.invalidate(groupDatesProvider(matchId));
            },
          ),
        ),
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
          title: const Text('Propose a date'),
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
            TextButton(onPressed: () => Navigator.pop(c, false), child: const Text('Cancel')),
            FilledButton(onPressed: () => Navigator.pop(c, true), child: const Text('Propose')),
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
