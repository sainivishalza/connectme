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

/// Turn-based truth or dare, as the signed-in player sees it.
class TdState {
  TdState({
    required this.active,
    this.round = 0,
    this.yourTurn = false,
    this.phase = '',
    this.kind = '',
    this.prompt = '',
    this.skipsLeft = 2,
    this.canTakeOver = false,
  });
  final bool active;
  final int round;
  final bool yourTurn;
  final String phase; // choose | answer
  final String kind; // truth | dare
  final String prompt;
  final int skipsLeft;
  final bool canTakeOver;

  factory TdState.fromJson(Map<String, dynamic> j) => TdState(
        active: jsonBool(j['active']),
        round: jsonInt(j['round']),
        yourTurn: jsonBool(j['your_turn']),
        phase: jsonString(j['phase']),
        kind: jsonString(j['kind']),
        prompt: jsonString(j['prompt']),
        skipsLeft: j['skips_left'] is num ? (j['skips_left'] as num).toInt() : 2,
        canTakeOver: jsonBool(j['can_take_over']),
      );
}

/// One round of group "Would you rather". Totals are anonymous.
class WyrState {
  WyrState({required this.active, this.pollId = '', this.a = '', this.b = '', this.closed = false, this.members = 0, this.voted = 0, this.myChoice = '', this.countA, this.countB});
  final bool active;
  final String pollId;
  final String a;
  final String b;
  final bool closed;
  final int members;
  final int voted;
  final String myChoice; // '' = not voted
  final int? countA; // shown once you have voted or the round is over
  final int? countB;

  factory WyrState.fromJson(Map<String, dynamic> j) => WyrState(
        active: jsonBool(j['active']),
        pollId: jsonString(j['poll_id']),
        a: jsonString(j['a']),
        b: jsonString(j['b']),
        closed: jsonBool(j['closed']),
        members: jsonInt(j['members']),
        voted: jsonInt(j['voted']),
        myChoice: jsonString(j['my_choice']),
        countA: j['count_a'] is num ? (j['count_a'] as num).toInt() : null,
        countB: j['count_b'] is num ? (j['count_b'] as num).toInt() : null,
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

  Future<WyrState> wyrState(String groupMatchId) async => WyrState.fromJson(await _api.getMap('/games/group/$groupMatchId'));
  Future<WyrState> wyrStart(String groupMatchId) async => WyrState.fromJson(await _api.postMap('/games/group/$groupMatchId/start'));
  Future<WyrState> wyrVote(String groupMatchId, String pollId, String choice) async =>
      WyrState.fromJson(await _api.postMap('/games/group/$groupMatchId/vote', body: {'poll_id': pollId, 'choice': choice}));

  Future<TdState> tdState(String matchId) async => TdState.fromJson(await _api.getMap('/games/td/$matchId'));
  Future<TdState> tdStart(String matchId) async => TdState.fromJson(await _api.postMap('/games/td/$matchId/start'));
  Future<TdState> tdPick(String matchId, String kind) async => TdState.fromJson(await _api.postMap('/games/td/$matchId/pick', body: {'kind': kind}));
  Future<TdState> tdDone(String matchId) async => TdState.fromJson(await _api.postMap('/games/td/$matchId/done'));
  Future<TdState> tdSkip(String matchId) async => TdState.fromJson(await _api.postMap('/games/td/$matchId/skip'));
  Future<TdState> tdTakeOver(String matchId) async => TdState.fromJson(await _api.postMap('/games/td/$matchId/take-over'));
  Future<void> tdEnd(String matchId) => _api.delete('/games/td/$matchId');

  Future<Quiz> answer(String quizId, int question, int choice) async =>
      Quiz.fromJson(await _api.postMap('/games/quiz/$quizId/answer', body: {'question': question, 'choice': choice}));
}

final gamesRepositoryProvider = Provider<GamesRepository>((ref) => GamesRepository(ref.watch(apiClientProvider)));
