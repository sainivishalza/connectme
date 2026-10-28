import 'package:flutter/material.dart';
import '../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'digest_repository.dart';

/// Where the link in the weekly email lands: one tap and the emails stop.
class DigestUnsubscribeScreen extends ConsumerStatefulWidget {
  const DigestUnsubscribeScreen({super.key, required this.token});
  final String token;

  @override
  ConsumerState<DigestUnsubscribeScreen> createState() => _DigestUnsubscribeScreenState();
}

class _DigestUnsubscribeScreenState extends ConsumerState<DigestUnsubscribeScreen> {
  String _state = 'ask'; // ask | done | failed
  bool _busy = false;

  Future<void> _go() async {
    setState(() => _busy = true);
    try {
      await ref.read(digestRepositoryProvider).unsubscribe(widget.token);
      if (mounted) setState(() => _state = 'done');
    } catch (_) {
      if (mounted) setState(() => _state = 'failed');
    }
    if (mounted) setState(() => _busy = false);
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return Scaffold(
      appBar: AppBar(title: Text(AppLocalizations.of(context)?.sWeeklySummary ?? 'Weekly summary')),
      body: Padding(
        padding: const EdgeInsets.all(24),
        child: switch (_state) {
          'done' => Text('Done. You will not get the weekly summary any more. You can turn it back on in Notifications.', key: const Key('unsub_done'), style: t.textTheme.bodyLarge),
          'failed' => Text('This link is not valid. Turn the summary off in Notifications inside the app.', key: const Key('unsub_failed'), style: t.textTheme.bodyLarge),
          _ => Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text('Stop the weekly summary?', style: t.textTheme.headlineSmall),
              const SizedBox(height: 16),
              FilledButton(key: const Key('unsub_go'), onPressed: _busy ? null : _go, child: Text(AppLocalizations.of(context)?.sUnsubscribe ?? 'Unsubscribe')),
            ]),
        },
      ),
    );
  }
}
