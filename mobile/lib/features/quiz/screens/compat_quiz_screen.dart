import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../quiz_repository.dart';

/// The compatibility quiz: one question at a time. People see only a rounded
/// percentage of how closely your answers line up with theirs, never the
/// answers themselves.
class CompatQuizScreen extends ConsumerStatefulWidget {
  const CompatQuizScreen({super.key});

  @override
  ConsumerState<CompatQuizScreen> createState() => _CompatQuizScreenState();
}

class _CompatQuizScreenState extends ConsumerState<CompatQuizScreen> {
  Map<String, int>? _answers;
  int _index = 0;

  Future<void> _pick(QuizState q, int choice) async {
    final question = q.questions[_index];
    final answers = _answers ??= Map.of(q.answers);
    final ok = await guarded(context, () => ref.read(quizRepositoryProvider).answer({question.id: choice}));
    if (!ok || !mounted) return;
    setState(() {
      answers[question.id] = choice;
      if (_index < q.questions.length - 1) _index++;
    });
  }

  @override
  Widget build(BuildContext context) {
    final v = ref.watch(quizProvider);
    return Scaffold(
      appBar: AppBar(title: Text(AppLocalizations.of(context)?.profileQuiz ?? 'Compatibility quiz')),
      body: v.when(
        loading: () => const Loader(),
        error: (e, _) => ErrorView(error: e, onRetry: () => ref.invalidate(quizProvider)),
        data: (q) {
          final answers = _answers ??= Map.of(q.answers);
          final t = Theme.of(context);
          final question = q.questions[_index];
          final done = answers.length;
          return ListView(padding: const EdgeInsets.all(20), children: [
            Text('$done of ${q.questions.length} answered', key: const Key('quiz_progress'), style: t.textTheme.labelLarge),
            const SizedBox(height: 8),
            LinearProgressIndicator(value: done / q.questions.length, minHeight: 8, borderRadius: BorderRadius.circular(6)),
            const SizedBox(height: 8),
            Text(
              done >= q.minShared
                  ? 'People you share ${q.minShared}+ answers with now show a match percentage, even before you see a photo.'
                  : 'Answer ${q.minShared - done} more to start seeing match percentages.',
              style: t.textTheme.bodySmall,
            ),
            const SizedBox(height: 24),
            Text(question.text, key: const Key('quiz_question'), style: t.textTheme.headlineSmall),
            const SizedBox(height: 16),
            for (var i = 0; i < question.options.length; i++)
              Padding(
                padding: const EdgeInsets.only(bottom: 10),
                child: answers[question.id] == i
                    ? FilledButton(key: Key('quiz_option_$i'), onPressed: () => _pick(q, i), child: Text(question.options[i]))
                    : OutlinedButton(key: Key('quiz_option_$i'), onPressed: () => _pick(q, i), child: Text(question.options[i])),
              ),
            Row(children: [
              TextButton(key: const Key('quiz_back'), onPressed: _index == 0 ? null : () => setState(() => _index--), child: Text(AppLocalizations.of(context)?.sBack ?? 'Back')),
              const Spacer(),
              TextButton(
                key: const Key('quiz_skip'),
                onPressed: _index >= q.questions.length - 1 ? null : () => setState(() => _index++),
                child: Text(AppLocalizations.of(context)?.sSkip ?? 'Skip'),
              ),
            ]),
          ]);
        },
      ),
    );
  }
}
