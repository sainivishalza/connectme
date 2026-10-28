import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../l10n/app_localizations.dart';
import '../../shared/widgets/states.dart';
import 'safety_repository.dart';

String reasonLabel(String reason) {
  final s = reason.replaceAll('_', ' ');
  return s.isEmpty ? s : s[0].toUpperCase() + s.substring(1);
}

/// What happened to the reports you filed. It says whether staff have looked
/// and whether they acted, never who was reported.
class MyReportsScreen extends ConsumerWidget {
  const MyReportsScreen({super.key});

  static String outcomeText(BuildContext context, String outcome) {
    final l = AppLocalizations.of(context);
    return switch (outcome) {
      'action_taken' => l?.sfOutcomeAction ?? 'Reviewed: we took action. Thank you for telling us.',
      'no_action' => l?.sfOutcomeNone ?? 'Reviewed: we found nothing that breaks the rules this time.',
      _ => l?.sfOutcomePending ?? 'Waiting for a moderator to look.',
    };
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final reports = ref.watch(myReportsProvider);
    final l = AppLocalizations.of(context);
    return Scaffold(
      appBar: AppBar(title: Text(l?.sfMyReports ?? 'My reports')),
      body: reports.when(
        loading: () => const Loader(),
        error: (e, _) => ErrorView(error: e, onRetry: () => ref.invalidate(myReportsProvider)),
        data: (list) => list.isEmpty
            ? EmptyState(
                title: l?.sfNoReportsTitle ?? 'No reports yet',
                message: l?.sfNoReportsBody ?? 'If someone breaks the rules, report them from their profile or the chat menu. You will see the result here.',
                icon: Icons.flag_outlined)
            : ListView(padding: const EdgeInsets.all(16), children: [
                for (final r in list)
                  Card(
                    child: ListTile(
                      key: Key('report_${r.id}'),
                      leading: Icon(r.outcome == 'pending' ? Icons.hourglass_empty : Icons.check_circle_outline),
                      title: Text(reasonLabel(r.reason)),
                      subtitle: Text('${r.createdAt.day}/${r.createdAt.month}/${r.createdAt.year}\n${outcomeText(context, r.outcome)}'),
                      isThreeLine: true,
                    ),
                  ),
              ]),
      ),
    );
  }
}
