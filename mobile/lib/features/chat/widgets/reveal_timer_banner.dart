import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../shared/widgets/app_snack.dart';
import '../../discovery/discovery_repository.dart';
import '../data/chat_models.dart';
import '../data/chat_repository.dart';

String revealClock(DateTime t) {
  final h = t.hour.toString().padLeft(2, '0'), m = t.minute.toString().padLeft(2, '0');
  final now = DateTime.now();
  final today = t.year == now.year && t.month == now.month && t.day == now.day;
  return today ? 'today at $h:$m' : '${t.day}/${t.month} at $h:$m';
}

/// A timed reveal in an anonymous chat: agree on a time, and photos and names
/// unlock for both of you then. Either of you can cancel before it happens.
class RevealTimerBanner extends ConsumerWidget {
  const RevealTimerBanner({super.key, required this.conv});
  final Conversation conv;

  Future<void> _run(BuildContext context, WidgetRef ref, Future<void> Function() f) async {
    final ok = await guarded(context, f);
    if (ok) ref.invalidate(conversationsProvider);
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final repo = ref.read(discoveryRepositoryProvider);
    final t = Theme.of(context);
    final rt = conv.revealTimer;
    Widget body;
    if (rt == null) {
      body = Row(children: [
        const Expanded(child: Text('Reveal together at a set time')),
        PopupMenuButton<int>(
          key: const Key('reveal_timer_ask'),
          tooltip: 'Choose when',
          onSelected: (h) => _run(context, ref, () => repo.proposeRevealTimer(conv.id, h)),
          itemBuilder: (_) => const [
            PopupMenuItem(value: 1, child: Text('In 1 hour')),
            PopupMenuItem(value: 24, child: Text('In 24 hours')),
            PopupMenuItem(value: 48, child: Text('In 2 days')),
          ],
          child: const Padding(padding: EdgeInsets.all(8), child: Text('Set a time')),
        ),
      ]);
    } else if (rt.accepted) {
      body = Row(children: [
        Expanded(child: Text('You both reveal ${revealClock(rt.at)}', key: const Key('reveal_timer_set'))),
        TextButton(key: const Key('reveal_timer_cancel'), onPressed: () => _run(context, ref, () => repo.cancelRevealTimer(conv.id)), child: Text(AppLocalizations.of(context)?.sCancel ?? 'Cancel')),
      ]);
    } else if (rt.needsReply) {
      body = Row(children: [
        Expanded(child: Text('They suggest revealing ${revealClock(rt.at)}', key: const Key('reveal_timer_offer'))),
        TextButton(key: const Key('reveal_timer_no'), onPressed: () => _run(context, ref, () => repo.cancelRevealTimer(conv.id)), child: Text(AppLocalizations.of(context)?.sNotNow ?? 'Not now')),
        FilledButton.tonal(key: const Key('reveal_timer_yes'), onPressed: () => _run(context, ref, () => repo.proposeRevealTimer(conv.id, 1)), child: Text(AppLocalizations.of(context)?.sAgree ?? 'Agree')),
      ]);
    } else {
      body = Row(children: [
        Expanded(child: Text('Waiting for them to agree to ${revealClock(rt.at)}', key: const Key('reveal_timer_waiting'))),
        TextButton(key: const Key('reveal_timer_cancel'), onPressed: () => _run(context, ref, () => repo.cancelRevealTimer(conv.id)), child: Text(AppLocalizations.of(context)?.sCancel ?? 'Cancel')),
      ]);
    }
    return Material(
      key: const Key('reveal_timer'),
      color: t.colorScheme.surfaceContainerHigh,
      child: Padding(padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 6), child: body),
    );
  }
}
