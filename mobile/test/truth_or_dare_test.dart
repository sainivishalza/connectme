import 'package:connectme/core/providers.dart';
import 'package:connectme/features/games/screens/truth_or_dare_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

/// A tiny in-memory stand-in for the server's two-player game.
class FakeGame {
  bool active = false;
  bool mine = true; // whose turn
  String phase = 'choose';
  String kind = '';
  String prompt = '';
  int round = 1;
  int skips = 2;
  bool takeOver = false;
  final calls = <String>[];

  Map<String, dynamic> json() => {
        'active': active,
        if (active) ...{'round': round, 'your_turn': mine, 'phase': phase, 'kind': kind, 'prompt': prompt, 'can_take_over': takeOver},
        'skips_left': skips,
      };

  Future<Reply> handle(dynamic o) async {
    final p = o.path as String;
    calls.add('${o.method} $p');
    if (!p.startsWith('/games/td/m1')) return Reply(404);
    final action = p.substring('/games/td/m1'.length);
    switch (action) {
      case '':
        if (o.method == 'DELETE') {
          active = false;
          return Reply(204);
        }
        return Reply(200, json());
      case '/start':
        active = true;
        return Reply(200, json());
      case '/pick':
        kind = (o.data as Map)['kind'] as String;
        prompt = kind == 'truth' ? 'What is your most useless talent?' : 'Do your best impression of a cat.';
        phase = 'answer';
        return Reply(200, json());
      case '/done' || '/skip':
        if (action == '/skip') skips--;
        mine = false;
        phase = 'choose';
        kind = '';
        prompt = '';
        round++;
        return Reply(200, json());
      case '/take-over':
        mine = true;
        takeOver = false;
        return Reply(200, json());
    }
    return Reply(404);
  }
}

Future<void> pump(WidgetTester t, FakeGame g) async {
  final client = fakeClient(g.handle);
  await t.pumpWidget(ProviderScope(
    overrides: [apiClientProvider.overrideWithValue(client)],
    child: const MaterialApp(home: TruthOrDareScreen(matchId: 'm1', pollInterval: Duration(seconds: 1))),
  ));
  await t.pump();
  await t.pump(const Duration(milliseconds: 100));
}

void main() {
  testWidgets('you choose, see the prompt, finish, and then it is their turn', (t) async {
    final g = FakeGame();
    await pump(t, g);
    expect(find.text('Your turn: truth or dare?'), findsOneWidget);
    expect(find.byKey(const Key('td_prompt')), findsNothing);

    await t.tap(find.byKey(const Key('td_truth')));
    await t.pump();
    await t.pump(const Duration(milliseconds: 100));
    expect(find.text('What is your most useless talent?'), findsOneWidget);
    expect(find.text('Skip (2 left)'), findsOneWidget);

    await t.tap(find.byKey(const Key('td_done')));
    await t.pump();
    await t.pump(const Duration(milliseconds: 100));
    expect(find.text('Their turn'), findsOneWidget);
    expect(find.text('They are choosing truth or dare.'), findsOneWidget);
    expect(find.byKey(const Key('td_truth')), findsNothing);
    await t.pumpWidget(const SizedBox());
  });

  testWidgets('skips run out, and the waiting player sees the other prompt without buttons', (t) async {
    final g = FakeGame()..active = true;
    await pump(t, g);
    await t.tap(find.byKey(const Key('td_dare')));
    await t.pump();
    await t.pump(const Duration(milliseconds: 100));
    await t.tap(find.byKey(const Key('td_skip')));
    await t.pump();
    await t.pump(const Duration(milliseconds: 100));
    expect(g.skips, 1);

    // Their turn now: they pick; we only see it and wait.
    g
      ..mine = false
      ..phase = 'answer'
      ..kind = 'truth'
      ..prompt = 'Which song do you secretly love?';
    await t.pump(const Duration(seconds: 1));
    await t.pump(const Duration(milliseconds: 100));
    expect(find.text('Which song do you secretly love?'), findsOneWidget);
    expect(find.byKey(const Key('td_done')), findsNothing);
    expect(find.text('Waiting for them to answer in the chat.'), findsOneWidget);

    // No skips left disables the button on our next turn.
    g
      ..mine = true
      ..skips = 0;
    await t.pump(const Duration(seconds: 1));
    await t.pump(const Duration(milliseconds: 100));
    expect(find.text('No skips left'), findsOneWidget);
    expect(t.widget<OutlinedButton>(find.byKey(const Key('td_skip'))).onPressed, isNull);
    await t.pumpWidget(const SizedBox());
  });

  testWidgets('an idle opponent can be taken over, and the game can be ended', (t) async {
    final g = FakeGame()
      ..active = true
      ..mine = false
      ..takeOver = true;
    await pump(t, g);
    expect(find.byKey(const Key('td_take_over')), findsOneWidget);
    await t.tap(find.byKey(const Key('td_take_over')));
    await t.pump();
    await t.pump(const Duration(milliseconds: 100));
    expect(find.text('Your turn: truth or dare?'), findsOneWidget);
    expect(find.byKey(const Key('td_take_over')), findsNothing);

    await t.tap(find.byKey(const Key('td_end')));
    await t.pumpAndSettle(const Duration(milliseconds: 100));
    await t.tap(find.byKey(const Key('td_end_confirm')));
    await t.pump();
    await t.pump(const Duration(milliseconds: 200));
    expect(g.calls.any((c) => c == 'DELETE /games/td/m1'), isTrue);
    await t.pumpWidget(const SizedBox());
  });
}
