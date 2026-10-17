import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/theme/tokens.dart';
import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../games_repository.dart';

/// The couple quiz for one match: pick a quiz, answer one question at a time,
/// wait for the other person if needed, then compare. What the other person
/// chose stays hidden until you have both finished.
class QuizScreen extends ConsumerStatefulWidget {
  const QuizScreen({super.key, required this.matchId, this.pollInterval = const Duration(seconds: 3)});
  final String matchId;
  final Duration pollInterval;

  @override
  ConsumerState<QuizScreen> createState() => _QuizScreenState();
}

class _QuizScreenState extends ConsumerState<QuizScreen> {
  late final GamesRepository _repo;
  Timer? _poll;
  Quiz? _quiz;
  List<QuizSetInfo> _sets = const [];
  Object? _error;
  bool _loading = true;
  bool _busy = false;
  int _index = 0;

  @override
  void initState() {
    super.initState();
    _repo = ref.read(gamesRepositoryProvider);
    _load();
    _poll = Timer.periodic(widget.pollInterval, (_) => _refresh());
  }

  Future<void> _load() async {
    try {
      final results = await Future.wait([_repo.current(widget.matchId), _repo.sets()]);
      if (!mounted) return;
      setState(() {
        _quiz = results[0] as Quiz?;
        _sets = results[1] as List<QuizSetInfo>;
        _loading = false;
        _error = null;
        _index = _firstUnanswered(_quiz);
      });
    } catch (e) {
      if (mounted) {
        setState(() {
          _error = e;
          _loading = false;
        });
      }
    }
  }

  int _firstUnanswered(Quiz? q) {
    if (q == null) return 0;
    final i = q.questions.indexWhere((x) => x.mine < 0);
    return i < 0 ? q.total : i;
  }

  /// While waiting for the other person, look for their progress.
  Future<void> _refresh() async {
    final q = _quiz;
    if (!mounted || q == null || q.complete || q.myAnswered < q.total) return;
    try {
      final latest = await _repo.current(widget.matchId);
      if (mounted && latest != null) setState(() => _quiz = latest);
    } catch (_) {
      // transient
    }
  }

  Future<void> _start(String setId) async {
    setState(() => _busy = true);
    try {
      final q = await _repo.start(widget.matchId, setId);
      if (mounted) {
        setState(() {
          _quiz = q;
          _index = _firstUnanswered(q);
        });
      }
    } catch (e) {
      if (mounted) showError(context, e);
    }
    if (mounted) setState(() => _busy = false);
  }

  Future<void> _answer(int choice) async {
    final q = _quiz;
    if (q == null) return;
    setState(() => _busy = true);
    try {
      final next = await _repo.answer(q.id, _index, choice);
      if (mounted) {
        setState(() {
          _quiz = next;
          _index = _index + 1;
        });
      }
    } catch (e) {
      if (mounted) showError(context, e);
    }
    if (mounted) setState(() => _busy = false);
  }

  @override
  void dispose() {
    _poll?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final q = _quiz;
    Widget body;
    if (_loading) {
      body = const Loader();
    } else if (_error != null) {
      body = ErrorView(error: _error!, onRetry: () {
        setState(() {
          _loading = true;
          _error = null;
        });
        _load();
      });
    } else if (q == null) {
      body = _Picker(sets: _sets, busy: _busy, onPick: _start);
    } else if (q.complete) {
      body = _Results(quiz: q, onAgain: () => setState(() => _quiz = null));
    } else if (_index >= q.total) {
      body = _Waiting(quiz: q);
    } else {
      body = _Question(quiz: q, index: _index, busy: _busy, onAnswer: _answer);
    }
    return Scaffold(appBar: AppBar(title: Text(q?.title ?? 'Couple quiz')), body: body);
  }
}

class _Picker extends StatelessWidget {
  const _Picker({required this.sets, required this.busy, required this.onPick});
  final List<QuizSetInfo> sets;
  final bool busy;
  final void Function(String id) onPick;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return ListView(
      padding: const EdgeInsets.fromLTRB(20, 4, 20, 20),
      children: [
        Text('Pick a quiz', style: t.textTheme.headlineSmall),
        const SizedBox(height: 6),
        Text('You both answer the same questions in your own time. Answers stay hidden until you have both finished.',
            style: t.textTheme.bodyMedium?.copyWith(color: t.colorScheme.onSurfaceVariant)),
        const SizedBox(height: 16),
        for (final s in sets)
          Padding(
            padding: const EdgeInsets.only(bottom: 10),
            child: Card(
              child: ListTile(
                key: Key('quiz_set_${s.id}'),
                title: Text(s.title),
                subtitle: Text('${s.questions} questions'),
                trailing: const Icon(Icons.chevron_right),
                onTap: busy ? null : () => onPick(s.id),
              ),
            ),
          ),
      ],
    );
  }
}

class _Question extends StatelessWidget {
  const _Question({required this.quiz, required this.index, required this.busy, required this.onAnswer});
  final Quiz quiz;
  final int index;
  final bool busy;
  final void Function(int choice) onAnswer;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final q = quiz.questions[index];
    return ListView(
      padding: const EdgeInsets.fromLTRB(20, 4, 20, 20),
      children: [
        ClipRRect(
          borderRadius: BorderRadius.circular(6),
          child: LinearProgressIndicator(value: index / quiz.total, minHeight: 6),
        ),
        const SizedBox(height: 8),
        Text('Question ${index + 1} of ${quiz.total}', style: t.textTheme.bodySmall),
        const SizedBox(height: 18),
        Text(q.text, key: const Key('quiz_question'), style: t.textTheme.headlineSmall),
        const SizedBox(height: 18),
        for (var i = 0; i < q.options.length; i++)
          Padding(
            padding: const EdgeInsets.only(bottom: 10),
            child: OutlinedButton(
              key: Key('quiz_option_$i'),
              style: OutlinedButton.styleFrom(minimumSize: const Size.fromHeight(56), alignment: Alignment.centerLeft),
              onPressed: busy ? null : () => onAnswer(i),
              child: Text(q.options[i]),
            ),
          ),
      ],
    );
  }
}

class _Waiting extends StatelessWidget {
  const _Waiting({required this.quiz});
  final Quiz quiz;

  @override
  Widget build(BuildContext context) {
    return EmptyState(
      icon: Icons.hourglass_top,
      title: 'Waiting for them',
      message: 'You are done. They have answered ${quiz.otherAnswered} of ${quiz.total}. '
          'You will see the comparison here as soon as they finish.',
    );
  }
}

class _Results extends StatelessWidget {
  const _Results({required this.quiz, required this.onAgain});
  final Quiz quiz;
  final VoidCallback onAgain;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final score = quiz.score ?? 0;
    final pct = quiz.total == 0 ? 0 : (score * 100 / quiz.total).round();
    return ListView(
      padding: const EdgeInsets.fromLTRB(20, 4, 20, 24),
      children: [
        Center(
          child: Container(
            width: 150,
            height: 150,
            decoration: BoxDecoration(shape: BoxShape.circle, gradient: ModeLight.daylight.gradient),
            alignment: Alignment.center,
            child: Column(mainAxisSize: MainAxisSize.min, children: [
              Text('$pct%', key: const Key('quiz_percent'), style: t.textTheme.displaySmall?.copyWith(color: ModeLight.daylight.ink)),
              Text('$score of ${quiz.total}', style: t.textTheme.labelLarge?.copyWith(color: ModeLight.daylight.ink)),
            ]),
          ),
        ),
        const SizedBox(height: 10),
        Text(
          'You answered the same way on $score of ${quiz.total}.',
          textAlign: TextAlign.center,
          style: t.textTheme.bodyLarge?.copyWith(color: t.colorScheme.onSurfaceVariant),
        ),
        const SizedBox(height: 20),
        for (final q in quiz.questions)
          Card(
            margin: const EdgeInsets.only(bottom: 10),
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Row(children: [
                  Expanded(child: Text(q.text, style: t.textTheme.titleSmall)),
                  Icon(q.same == true ? Icons.check_circle : Icons.remove_circle_outline,
                      color: q.same == true ? const Color(0xFF1F9D63) : t.colorScheme.outline),
                ]),
                const SizedBox(height: 8),
                Text('You: ${q.mine >= 0 ? q.options[q.mine] : '-'}', style: t.textTheme.bodyMedium),
                Text('Them: ${q.theirs != null ? q.options[q.theirs!] : '-'}', style: t.textTheme.bodyMedium),
              ]),
            ),
          ),
        const SizedBox(height: 8),
        OutlinedButton(key: const Key('quiz_again'), onPressed: onAgain, child: const Text('Play another quiz')),
      ],
    );
  }
}
