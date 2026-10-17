import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models/json.dart';
import '../../core/network/api_client.dart';
import '../../core/providers.dart';

class GamePrompt {
  GamePrompt(this.kind, this.text);
  final String kind; // truth | dare | icebreaker
  final String text;

  factory GamePrompt.fromJson(Map<String, dynamic> j) => GamePrompt(jsonString(j['kind']), jsonString(j['text']));
}

class QuizSetInfo {
  QuizSetInfo({required this.id, required this.title, required this.questions});
  final String id;
  final String title;
  final int questions;

  factory QuizSetInfo.fromJson(Map<String, dynamic> j) =>
      QuizSetInfo(id: jsonString(j['id']), title: jsonString(j['title']), questions: jsonInt(j['questions']));
}

class QuizQuestion {
  QuizQuestion({required this.text, required this.options, required this.mine, this.theirs, this.same});
  final String text;
  final List<String> options;
  final int mine; // -1 = not answered
  final int? theirs; // only once both have finished
  final bool? same;

  factory QuizQuestion.fromJson(Map<String, dynamic> j) => QuizQuestion(
        text: jsonString(j['text']),
        options: jsonStrings(j['options']),
        mine: j['mine'] is num ? (j['mine'] as num).toInt() : -1,
        theirs: j['theirs'] is num ? (j['theirs'] as num).toInt() : null,
        same: j['same'] is bool ? j['same'] as bool : null,
      );
}

class Quiz {
  Quiz({
    required this.id,
    required this.matchId,
    required this.title,
    required this.questions,
    required this.myAnswered,
    required this.otherAnswered,
    required this.complete,
    this.score,
  });
  final String id;
  final String matchId;
  final String title;
  final List<QuizQuestion> questions;
  final int myAnswered;
  final int otherAnswered;
  final bool complete;
  final int? score;

  int get total => questions.length;

  factory Quiz.fromJson(Map<String, dynamic> j) => Quiz(
        id: jsonString(j['id']),
        matchId: jsonString(j['match_id']),
        title: jsonString(j['title']),
        questions: listOf(j['questions'], QuizQuestion.fromJson),
        myAnswered: jsonInt(j['my_answered']),
        otherAnswered: jsonInt(j['other_answered']),
        complete: jsonBool(j['complete']),
        score: j['score'] is num ? (j['score'] as num).toInt() : null,
      );
}

class GamesRepository {
  GamesRepository(this._api);
  final ApiClient _api;

  Future<GamePrompt> prompt(String kind) async => GamePrompt.fromJson(await _api.getMap('/games/prompt', query: {'kind': kind}));

  Future<List<QuizSetInfo>> sets() async => listOf((await _api.getMap('/games/quiz/sets'))['sets'], QuizSetInfo.fromJson);

  Future<Quiz> start(String matchId, String setId) async =>
      Quiz.fromJson(await _api.postMap('/games/quiz', body: {'match_id': matchId, 'set_id': setId}));

  /// The latest quiz in this match, or null if none has been started.
  Future<Quiz?> current(String matchId) async {
    final q = (await _api.getMap('/games/quiz/match/$matchId'))['quiz'];
    return q is Map ? Quiz.fromJson(Map<String, dynamic>.from(q)) : null;
  }

  Future<Quiz> answer(String quizId, int question, int choice) async =>
      Quiz.fromJson(await _api.postMap('/games/quiz/$quizId/answer', body: {'question': question, 'choice': choice}));
}

final gamesRepositoryProvider = Provider<GamesRepository>((ref) => GamesRepository(ref.watch(apiClientProvider)));
