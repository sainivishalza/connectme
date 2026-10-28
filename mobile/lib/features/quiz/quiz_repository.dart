import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models/json.dart';
import '../../core/network/api_client.dart';
import '../../core/providers.dart';

class QuizQuestion {
  QuizQuestion({required this.id, required this.text, required this.options});
  final String id;
  final String text;
  final List<String> options;

  factory QuizQuestion.fromJson(Map<String, dynamic> j) =>
      QuizQuestion(id: jsonString(j['id']), text: jsonString(j['text']), options: jsonStrings(j['options']));
}

class QuizState {
  QuizState({required this.questions, required this.answers, required this.minShared});
  final List<QuizQuestion> questions;
  final Map<String, int> answers;
  final int minShared;
}

class QuizRepository {
  QuizRepository(this._api);
  final ApiClient _api;

  Future<QuizState> load() async {
    final m = await _api.getMap('/quiz/');
    final raw = m['answers'] is Map ? Map<String, dynamic>.from(m['answers'] as Map) : <String, dynamic>{};
    return QuizState(
      questions: listOf(m['questions'], QuizQuestion.fromJson),
      answers: {for (final e in raw.entries) e.key: jsonInt(e.value)},
      minShared: jsonInt(m['min_shared']),
    );
  }

  Future<void> answer(Map<String, int> answers) => _api.put('/quiz/answers', body: {'answers': answers});
}

final quizRepositoryProvider = Provider<QuizRepository>((ref) => QuizRepository(ref.watch(apiClientProvider)));

final quizProvider = FutureProvider.autoDispose<QuizState>((ref) => ref.watch(quizRepositoryProvider).load());
