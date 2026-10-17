import 'package:connectme/core/providers.dart';
import 'package:connectme/features/games/screens/quiz_screen.dart';
import 'package:connectme/features/games/widgets/games_sheet.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';

import 'helpers.dart';

Map<String, dynamic> quizJson({
  int mine = 0,
  int other = 0,
  bool complete = false,
  int total = 3,
  List<int>? myChoices,
  List<int>? theirChoices,
}) {
  final my = myChoices ?? List.generate(mine, (_) => 0);
  return {
    'id': 'q1',
    'match_id': 'm1',
    'set_id': 'everyday',
    'title': 'Everyday life',
    'my_answered': my.length,
    'other_answered': complete ? total : other,
    'complete': complete,
    if (complete) 'score': 2,
    'questions': [
      for (var i = 0; i < total; i++)
        {
          'text': 'Question ${i + 1}',
          'options': ['A', 'B'],
          'mine': i < my.length ? my[i] : -1,
          if (complete) 'theirs': theirChoices![i],
          if (complete) 'same': theirChoices![i] == my[i],
        },
    ],
  };
}

void main() {
  group('games sheet', () {
    Future<List<String>> pumpSheet(WidgetTester t, {required String? matchId, required Handler h}) async {
      final client = fakeClient(h);
      final sent = <String>[];
      final router = GoRouter(initialLocation: '/chat', routes: [
        GoRoute(
          path: '/chat',
          builder: (_, _) => Builder(
            builder: (c) => Scaffold(
              body: TextButton(
                key: const Key('open_games'),
                onPressed: () => showGamesSheet(c, matchId: matchId, sendText: (s) async => sent.add(s)),
                child: const Text('games'),
              ),
            ),
          ),
        ),
        GoRoute(path: '/quiz/:id', builder: (_, s) => Scaffold(body: Text('quiz ${s.pathParameters['id']}', key: const Key('quiz_page')))),
      ]);
      await t.pumpWidget(ProviderScope(
        overrides: [apiClientProvider.overrideWithValue(client)],
        child: MaterialApp.router(routerConfig: router),
      ));
      await t.tap(find.byKey(const Key('open_games')));
      await t.pumpAndSettle();
      return sent;
    }

    testWidgets('draws a truth, offers another, and sends it to the chat', (t) async {
      var n = 0;
      final sent = await pumpSheet(t, matchId: 'm1', h: (o) async {
        if (o.path == '/games/prompt') {
          n++;
          return Reply(200, {'kind': o.queryParameters['kind'], 'text': 'Prompt number $n'});
        }
        return Reply(404, {'error': 'nope'});
      });
      expect(find.byKey(const Key('game_prompt')), findsNothing);
      await t.tap(find.byKey(const Key('game_truth')));
      await t.pumpAndSettle();
      expect(find.text('Prompt number 1'), findsOneWidget);
      await t.tap(find.byKey(const Key('game_another')));
      await t.pumpAndSettle();
      expect(find.text('Prompt number 2'), findsOneWidget);
      await t.tap(find.byKey(const Key('game_send')));
      await t.pumpAndSettle();
      expect(sent, ['Truth: Prompt number 2']);
      expect(find.byKey(const Key('game_prompt')), findsNothing, reason: 'sheet closed after sending');
    });

    testWidgets('the quiz entry opens the quiz for this match, and is absent in group chats', (t) async {
      await pumpSheet(t, matchId: 'm1', h: (o) async => Reply(404, {'error': 'x'}));
      await t.tap(find.byKey(const Key('game_quiz')));
      await t.pumpAndSettle();
      expect(find.text('quiz m1'), findsOneWidget);
    });

    testWidgets('group chats get prompts only', (t) async {
      await pumpSheet(t, matchId: null, h: (o) async => Reply(404, {'error': 'x'}));
      expect(find.byKey(const Key('game_dare')), findsOneWidget);
      expect(find.byKey(const Key('game_quiz')), findsNothing);
    });
  });

  group('quiz screen', () {
    Future<FakeAdapter> pumpQuiz(WidgetTester t, Handler h) async {
      final client = fakeClient(h);
      await t.pumpWidget(ProviderScope(
        overrides: [apiClientProvider.overrideWithValue(client)],
        child: const MaterialApp(home: QuizScreen(matchId: 'm1', pollInterval: Duration(milliseconds: 100))),
      ));
      await t.pumpAndSettle();
      return client.dio.httpClientAdapter as FakeAdapter;
    }

    testWidgets('pick a quiz, answer every question, wait, then see the comparison', (t) async {
      final my = <int>[];
      var started = false;
      var finished = false;
      final adapter = await pumpQuiz(t, (o) async {
        if (o.path == '/games/quiz/match/m1') {
          if (finished) return Reply(200, {'quiz': quizJson(complete: true, myChoices: [0, 1, 0], theirChoices: [0, 0, 0])});
          return Reply(200, {'quiz': started ? quizJson(myChoices: my, other: 1) : null});
        }
        if (o.path == '/games/quiz/sets') {
          return Reply(200, {
            'sets': [
              {'id': 'everyday', 'title': 'Everyday life', 'questions': 3}
            ]
          });
        }
        if (o.path == '/games/quiz') {
          started = true;
          return Reply(200, quizJson());
        }
        if (o.path == '/games/quiz/q1/answer') {
          final d = o.data as Map;
          my.add(d['choice'] as int);
          return Reply(200, quizJson(myChoices: my, other: 1));
        }
        return Reply(404, {'error': 'nope'});
      });

      expect(find.text('Pick a quiz'), findsOneWidget);
      await t.tap(find.byKey(const Key('quiz_set_everyday')));
      await t.pumpAndSettle();
      expect(find.text('Question 1 of 3'), findsOneWidget);

      for (final choice in [0, 1, 0]) {
        await t.tap(find.byKey(Key('quiz_option_$choice')));
        await t.pumpAndSettle();
      }
      expect(find.text('Waiting for them'), findsOneWidget);
      expect(find.textContaining('answered 1 of 3'), findsOneWidget, reason: 'only a count, never their choices');
      expect(adapter.requests.where((r) => r.path == '/games/quiz/q1/answer').length, 3);

      finished = true;
      await t.pump(const Duration(milliseconds: 250));
      await t.pumpAndSettle();
      expect(find.byKey(const Key('quiz_percent')), findsOneWidget);
      expect(find.text('67%'), findsOneWidget);
      expect(find.text('You answered the same way on 2 of 3.'), findsOneWidget);
      await t.scrollUntilVisible(find.byKey(const Key('quiz_again')), 300, scrollable: find.byType(Scrollable).first);
      expect(find.byKey(const Key('quiz_again')), findsOneWidget);
    });

    testWidgets('resumes at the first unanswered question', (t) async {
      await pumpQuiz(t, (o) async {
        if (o.path == '/games/quiz/match/m1') return Reply(200, {'quiz': quizJson(myChoices: [1], other: 0)});
        if (o.path == '/games/quiz/sets') return Reply(200, {'sets': []});
        return Reply(404, {'error': 'nope'});
      });
      expect(find.text('Question 2 of 3'), findsOneWidget);
    });

    testWidgets('a server error shows a retry instead of a blank screen', (t) async {
      await pumpQuiz(t, (o) async => Reply(500, {'error': 'boom'}));
      expect(find.text('Try again'), findsOneWidget);
    });
  });
}
