import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';

import '../../../core/models/json.dart';

/// A live poll as the room shows it.
class LivePoll {
  const LivePoll({required this.id, required this.question, required this.options, required this.counts, this.myChoice, this.closed = false});
  final String id;
  final String question;
  final List<String> options;
  final List<int> counts;
  final int? myChoice;
  final bool closed;

  int get total => counts.fold(0, (a, b) => a + b);

  static LivePoll? fromJson(Object? o) {
    if (o is! Map) return null;
    final j = Map<String, dynamic>.from(o);
    return LivePoll(
      id: jsonString(j['id']),
      question: jsonString(j['question']),
      options: jsonStrings(j['options']),
      counts: (j['counts'] is List ? (j['counts'] as List).map((e) => jsonInt(e)).toList() : <int>[]),
      myChoice: j['my_choice'] is num ? jsonInt(j['my_choice']) : null,
      closed: jsonBool(j['closed']),
    );
  }
}

/// The poll over the video: tap an answer to vote, then watch the bars move.
/// The host sees the bars straight away and a button to end the poll.
class LivePollCard extends StatelessWidget {
  const LivePollCard({super.key, required this.poll, this.onVote, this.onClose, this.isHost = false});
  final LivePoll poll;
  final void Function(int choice)? onVote;
  final VoidCallback? onClose;
  final bool isHost;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final showBars = poll.myChoice != null || poll.closed || isHost;
    return DecoratedBox(
      key: const Key('live_poll'),
      decoration: BoxDecoration(color: Colors.black.withValues(alpha: 0.72), borderRadius: BorderRadius.circular(14)),
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.start, children: [
          Row(children: [
            Expanded(child: Text(poll.question, style: t.textTheme.titleSmall?.copyWith(color: Colors.white))),
            if (isHost && !poll.closed) TextButton(key: const Key('poll_close'), onPressed: onClose, child: Text(AppLocalizations.of(context)?.sEnd ?? 'End')),
            if (poll.closed) Text('Ended', style: t.textTheme.labelMedium?.copyWith(color: Colors.white70)),
          ]),
          const SizedBox(height: 6),
          for (var i = 0; i < poll.options.length; i++)
            Padding(
              padding: const EdgeInsets.only(bottom: 6),
              child: InkWell(
                key: Key('poll_option_$i'),
                borderRadius: BorderRadius.circular(10),
                onTap: (!showBars && !poll.closed && onVote != null) ? () => onVote!(i) : null,
                child: Stack(children: [
                  if (showBars)
                    Positioned.fill(
                      child: FractionallySizedBox(
                        alignment: Alignment.centerLeft,
                        widthFactor: poll.total == 0 ? 0 : (poll.counts.length > i ? poll.counts[i] : 0) / poll.total,
                        child: DecoratedBox(decoration: BoxDecoration(color: Colors.pinkAccent.withValues(alpha: 0.45), borderRadius: BorderRadius.circular(10))),
                      ),
                    ),
                  Container(
                    decoration: BoxDecoration(border: Border.all(color: poll.myChoice == i ? Colors.pinkAccent : Colors.white24), borderRadius: BorderRadius.circular(10)),
                    padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
                    child: Row(children: [
                      Expanded(child: Text(poll.options[i], style: const TextStyle(color: Colors.white))),
                      if (showBars) Text('${poll.counts.length > i ? poll.counts[i] : 0}', key: Key('poll_count_$i'), style: const TextStyle(color: Colors.white)),
                    ]),
                  ),
                ]),
              ),
            ),
        ]),
      ),
    );
  }
}
