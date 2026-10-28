import 'dart:async';

import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/theme/tokens.dart';
import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../games_repository.dart';
import '../../../core/realtime/live_refresh.dart';

/// Truth or Dare, taking turns. The player whose turn it is picks truth or
/// dare and gets a prompt; the other person sees it too. The answer goes in the
/// chat (end-to-end encrypted like everything else there), then "Done" or
/// "Skip" passes the turn.
class TruthOrDareScreen extends ConsumerStatefulWidget {
  const TruthOrDareScreen({super.key, required this.matchId, this.pollInterval = const Duration(seconds: 2)});
  final String matchId;
  final Duration pollInterval;

  @override
  ConsumerState<TruthOrDareScreen> createState() => _TruthOrDareScreenState();
}

class _TruthOrDareScreenState extends ConsumerState<TruthOrDareScreen> {
  late final GamesRepository _repo;
  TdState? _state;
  Object? _error;
  LiveRefresh? _live;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _repo = ref.read(gamesRepositoryProvider);
    _start();
  }

  Future<void> _start() async {
    try {
      final s = await _repo.tdStart(widget.matchId);
      if (!mounted) return;
      setState(() {
        _state = s;
        _error = null;
      });
      _live ??= LiveRefresh.of(ref, kind: 'game', id: widget.matchId, interval: widget.pollInterval, onTick: _poll).start();
    } catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  Future<void> _poll() async {
    if (_busy || !mounted) return;
    try {
      final s = await _repo.tdState(widget.matchId);
      if (mounted) setState(() => _state = s);
    } catch (_) {}
  }

  @override
  void dispose() {
    _live?.dispose();
    super.dispose();
  }

  Future<void> _act(Future<TdState> Function() f) async {
    setState(() => _busy = true);
    try {
      final s = await f();
      if (mounted) setState(() => _state = s);
    } catch (e) {
      if (mounted) showError(context, e);
    }
    if (mounted) setState(() => _busy = false);
  }

  Future<void> _end() async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (c) => AlertDialog(
        title: const Text('End the game for both of you?'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(c, false), child: const Text('Keep playing')),
          FilledButton(key: const Key('td_end_confirm'), onPressed: () => Navigator.pop(c, true), child: Text(AppLocalizations.of(context)?.sEndGame ?? 'End game')),
        ],
      ),
    );
    if (ok != true || !mounted) return;
    if (await guarded(context, () => _repo.tdEnd(widget.matchId)) && mounted) Navigator.of(context).maybePop();
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final s = _state;
    Widget body;
    if (_error != null) {
      body = ErrorView(error: _error!, onRetry: _start);
    } else if (s == null) {
      body = const Loader();
    } else if (!s.active) {
      body = EmptyState(icon: Icons.theater_comedy_outlined, title: 'The game ended', message: 'Start another whenever you like.', action: FilledButton(onPressed: _start, child: Text(AppLocalizations.of(context)?.sPlayAgain ?? 'Play again')));
    } else {
      final light = ModeLight.daylight;
      body = ListView(padding: const EdgeInsets.all(20), children: [
        Text('Round ${s.round}', style: t.textTheme.labelLarge),
        const SizedBox(height: 6),
        Text(
          s.yourTurn ? (s.phase == 'choose' ? 'Your turn: truth or dare?' : 'Your turn') : 'Their turn',
          key: const Key('td_turn'),
          style: t.textTheme.headlineMedium,
        ),
        const SizedBox(height: 18),
        if (s.phase == 'answer') ...[
          Container(
            key: const Key('td_prompt'),
            padding: const EdgeInsets.all(22),
            decoration: BoxDecoration(gradient: light.wash(t.brightness), borderRadius: BorderRadius.circular(Radii.card)),
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text(s.kind == 'dare' ? 'Dare' : 'Truth', style: t.textTheme.labelLarge),
              const SizedBox(height: 8),
              Text(s.prompt, style: t.textTheme.titleLarge),
            ]),
          ),
          const SizedBox(height: 14),
          if (s.yourTurn) ...[
            Text('Answer or do it in the chat, then tap Done.', style: t.textTheme.bodyMedium),
            const SizedBox(height: 12),
            Row(children: [
              Expanded(
                child: OutlinedButton(
                  key: const Key('td_skip'),
                  onPressed: _busy || s.skipsLeft == 0 ? null : () => _act(() => _repo.tdSkip(widget.matchId)),
                  child: Text(s.skipsLeft == 0 ? 'No skips left' : 'Skip (${s.skipsLeft} left)'),
                ),
              ),
              const SizedBox(width: 12),
              Expanded(child: FilledButton(key: const Key('td_done'), onPressed: _busy ? null : () => _act(() => _repo.tdDone(widget.matchId)), child: Text(AppLocalizations.of(context)?.sDone ?? 'Done'))),
            ]),
          ] else
            Text('Waiting for them to answer in the chat.', style: t.textTheme.bodyMedium),
        ] else if (s.yourTurn)
          Row(children: [
            Expanded(child: FilledButton(key: const Key('td_truth'), onPressed: _busy ? null : () => _act(() => _repo.tdPick(widget.matchId, 'truth')), child: Text(AppLocalizations.of(context)?.sTruth ?? 'Truth'))),
            const SizedBox(width: 12),
            Expanded(child: FilledButton.tonal(key: const Key('td_dare'), onPressed: _busy ? null : () => _act(() => _repo.tdPick(widget.matchId, 'dare')), child: Text(AppLocalizations.of(context)?.sDare ?? 'Dare'))),
          ])
        else
          Text('They are choosing truth or dare.', style: t.textTheme.bodyMedium),
        if (s.canTakeOver) ...[
          const SizedBox(height: 18),
          Card(
            child: ListTile(
              title: const Text('They have been away for a while'),
              subtitle: const Text('You can take the next turn.'),
              trailing: FilledButton(key: const Key('td_take_over'), onPressed: _busy ? null : () => _act(() => _repo.tdTakeOver(widget.matchId)), child: Text(AppLocalizations.of(context)?.sTakeOver ?? 'Take over')),
            ),
          ),
        ],
      ]);
    }
    return Scaffold(
      appBar: AppBar(
        title: Text(AppLocalizations.of(context)?.sTruthOrDare2 ?? 'Truth or dare'),
        actions: [if (s?.active == true) IconButton(key: const Key('td_end'), tooltip: 'End game', icon: const Icon(Icons.stop_circle_outlined), onPressed: _end)],
      ),
      body: body,
    );
  }
}
