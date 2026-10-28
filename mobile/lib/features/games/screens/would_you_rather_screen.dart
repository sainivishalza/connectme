import 'dart:async';

import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../games_repository.dart';
import '../../../core/realtime/live_refresh.dart';

/// "Would you rather" for a matched group chat. Everyone in either group votes
/// A or B; the totals appear once you have voted, and nobody can see who chose
/// what.
class WouldYouRatherScreen extends ConsumerStatefulWidget {
  const WouldYouRatherScreen({super.key, required this.groupMatchId, this.pollInterval = const Duration(seconds: 3)});
  final String groupMatchId;
  final Duration pollInterval;

  @override
  ConsumerState<WouldYouRatherScreen> createState() => _WouldYouRatherScreenState();
}

class _WouldYouRatherScreenState extends ConsumerState<WouldYouRatherScreen> {
  late final GamesRepository _repo;
  WyrState? _state;
  Object? _error;
  LiveRefresh? _live;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _repo = ref.read(gamesRepositoryProvider);
    _load();
    _live = LiveRefresh.of(ref, kind: 'game', id: widget.groupMatchId, interval: widget.pollInterval, onTick: _poll).start();
  }

  @override
  void dispose() {
    _live?.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final s = await _repo.wyrState(widget.groupMatchId);
      if (mounted) {
        setState(() {
          _state = s;
          _error = null;
        });
      }
    } catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  Future<void> _poll() async {
    if (_busy || !mounted) return;
    try {
      final s = await _repo.wyrState(widget.groupMatchId);
      if (mounted) setState(() => _state = s);
    } catch (_) {}
  }

  Future<void> _act(Future<WyrState> Function() f) async {
    setState(() => _busy = true);
    try {
      final s = await f();
      if (mounted) setState(() => _state = s);
    } catch (e) {
      if (mounted) showError(context, e);
    }
    if (mounted) setState(() => _busy = false);
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final s = _state;
    Widget body;
    if (_error != null) {
      body = ErrorView(error: _error!, onRetry: _load);
    } else if (s == null) {
      body = const Loader();
    } else if (!s.active) {
      body = EmptyState(
        icon: Icons.how_to_vote_outlined,
        title: 'Would you rather',
        message: 'Everyone in the chat picks A or B. You only see the totals, never who chose what.',
        action: FilledButton(key: const Key('wyr_start'), onPressed: _busy ? null : () => _act(() => _repo.wyrStart(widget.groupMatchId)), child: Text(AppLocalizations.of(context)?.sStartARound ?? 'Start a round')),
      );
    } else {
      final voted = s.myChoice.isNotEmpty;
      body = ListView(padding: const EdgeInsets.all(20), children: [
        Text(s.closed ? 'Round finished' : '${s.voted} of ${s.members} have voted', key: const Key('wyr_status'), style: t.textTheme.labelLarge),
        const SizedBox(height: 14),
        _option(t, 'a', s.a, s, voted),
        const SizedBox(height: 12),
        Center(child: Text('or', style: t.textTheme.bodyMedium)),
        const SizedBox(height: 12),
        _option(t, 'b', s.b, s, voted),
        const SizedBox(height: 20),
        if (!voted && !s.closed) Text('Pick one to see how everyone else voted.', style: t.textTheme.bodySmall),
        if (s.closed)
          FilledButton(key: const Key('wyr_next'), onPressed: _busy ? null : () => _act(() => _repo.wyrStart(widget.groupMatchId)), child: Text(AppLocalizations.of(context)?.sNextQuestion ?? 'Next question')),
      ]);
    }
    return Scaffold(appBar: AppBar(title: Text(AppLocalizations.of(context)?.sWouldYouRather ?? 'Would you rather')), body: body);
  }

  Widget _option(ThemeData t, String key, String text, WyrState s, bool voted) {
    final count = key == 'a' ? s.countA : s.countB;
    final total = (s.countA ?? 0) + (s.countB ?? 0);
    final share = count == null || total == 0 ? 0.0 : count / total;
    final mine = s.myChoice == key;
    final canVote = !voted && !s.closed && !_busy;
    return Card(
      key: Key('wyr_$key'),
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(16),
        side: BorderSide(color: mine ? t.colorScheme.primary : t.colorScheme.outlineVariant, width: mine ? 2 : 1),
      ),
      child: InkWell(
        borderRadius: BorderRadius.circular(16),
        onTap: canVote ? () => _act(() => _repo.wyrVote(widget.groupMatchId, s.pollId, key)) : null,
        child: Padding(
          padding: const EdgeInsets.all(18),
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Text(text, style: t.textTheme.titleMedium),
            if (count != null) ...[
              const SizedBox(height: 10),
              LinearProgressIndicator(value: share, minHeight: 8, borderRadius: BorderRadius.circular(4)),
              const SizedBox(height: 6),
              Text('$count ${count == 1 ? 'vote' : 'votes'}${mine ? ' · your pick' : ''}', key: Key('wyr_count_$key'), style: t.textTheme.bodySmall),
            ],
          ]),
        ),
      ),
    );
  }
}
