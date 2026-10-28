import 'package:connectme/core/providers.dart';
import 'package:connectme/features/games/screens/party_game_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

Map<String, dynamic> game({bool mine = true, String phase = 'choose', String kind = '', String prompt = ''}) => {
      'active': true,
      'your_turn': mine,
      'phase': phase,
      'kind': kind,
      'prompt': prompt,
      'skips_left': 2,
      'players': [
        {'label': 'Mina', 'you': mine, 'turn': mine},
        {'label': 'Member A', 'you': !mine, 'turn': !mine},
        {'label': 'Leo'},
      ],
    };

void main() {
  testWidgets('group truth or dare: start, pick, see the prompt, finish', (t) async {
    t.view.physicalSize = const Size(800, 2000);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.reset);
    var step = 0;
    final calls = <String>[];
    final client = fakeClient((o) async {
      if (o.path == '/games/party/g1' && o.method == 'GET') return Reply(200, step == 0 ? {'active': false, 'skips_left': 2} : game(phase: step == 1 ? 'choose' : 'answer', kind: 'truth', prompt: 'Your best holiday?'));
      if (o.path == '/games/party/g1/start') {
        step = 1;
        calls.add('start');
        return Reply(200, game());
      }
      if (o.path == '/games/party/g1/pick') {
        step = 2;
        calls.add('pick ${(o.data as Map)['kind']}');
        return Reply(200, game(phase: 'answer', kind: 'truth', prompt: 'Your best holiday?'));
      }
      if (o.path == '/games/party/g1/done') {
        step = 1;
        calls.add('done');
        return Reply(200, game(mine: false));
      }
      return Reply(404);
    });
    await t.pumpWidget(ProviderScope(overrides: [apiClientProvider.overrideWithValue(client)], child: const MaterialApp(home: GroupTruthOrDareScreen(groupMatchId: 'g1', pollInterval: Duration(seconds: 30)))));
    await t.pumpAndSettle();
    await t.tap(find.byKey(const Key('party_start')));
    await t.pumpAndSettle();
    expect(find.byKey(const Key('party_turn')), findsOneWidget);
    expect(find.text('Mina (you)'), findsOneWidget);
    await t.tap(find.byKey(const Key('pick_truth')));
    await t.pumpAndSettle();
    expect(find.text('Your best holiday?'), findsOneWidget);
    await t.tap(find.byKey(const Key('party_done')));
    await t.pumpAndSettle();
    expect(find.byKey(const Key('party_waiting')), findsOneWidget);
    expect(calls, ['start', 'pick truth', 'done']);
    await t.pumpWidget(const SizedBox());
  });

  testWidgets('masked room: join, wait, get seated under a made-up name, answer a truth, report another answer', (t) async {
    t.view.physicalSize = const Size(800, 2400);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.reset);
    var seated = false;
    final posted = <Object?>[];
    final reports = <String>[];
    Map<String, dynamic> room({bool answered = false}) => {
          'room_id': 'r1',
          'ends_at': DateTime.now().add(const Duration(minutes: 20)).toUtc().toIso8601String(),
          'my_alias': 'Quiet Fox',
          'game': game(phase: 'answer', kind: 'truth', prompt: 'A habit you are proud of?'),
          'answers': [
            {'id': 'a1', 'alias': 'Calm Owl', 'kind': 'truth', 'prompt': 'Best meal?', 'body': 'Dumplings', 'mine': false},
            if (answered) {'id': 'a2', 'alias': 'Quiet Fox', 'kind': 'truth', 'prompt': 'A habit you are proud of?', 'body': 'Reading daily', 'mine': true},
          ],
        };
    final client = fakeClient((o) async {
      if (o.path == '/games/masked/join') return Reply(200, {'state': 'waiting', 'waiting': 2});
      if (o.path == '/games/masked/status') {
        seated = true;
        return Reply(200, {'state': 'in_room', 'room_id': 'r1'});
      }
      if (o.path == '/games/masked/rooms/r1' && o.method == 'GET') return Reply(200, room());
      if (o.path == '/games/masked/rooms/r1/finish') {
        posted.add(o.data);
        return Reply(200, room(answered: true));
      }
      if (o.path == '/games/masked/rooms/r1/answers/a1/report') {
        reports.add('a1');
        return Reply(204);
      }
      return Reply(404);
    });
    await t.pumpWidget(ProviderScope(overrides: [apiClientProvider.overrideWithValue(client)], child: const MaterialApp(home: MaskedRoomScreen(pollInterval: Duration(milliseconds: 200)))));
    await t.pumpAndSettle();
    await t.tap(find.byKey(const Key('masked_join')));
    await t.pump(const Duration(milliseconds: 60));
    expect(find.byKey(const Key('masked_waiting')), findsOneWidget);
    await t.pump(const Duration(milliseconds: 600));
    expect(seated, isTrue);
    await t.pump(const Duration(milliseconds: 600));
    expect(find.text('You are Quiet Fox'), findsOneWidget);
    expect(find.textContaining('Not end-to-end encrypted'), findsWidgets);

    await t.enterText(find.byKey(const Key('masked_answer')), 'Reading daily');
    await t.tap(find.byKey(const Key('party_done')));
    await t.pump(const Duration(milliseconds: 300));
    expect(posted.single, {'text': 'Reading daily', 'skip': false});
    await t.tap(find.byKey(const Key('report_a1')));
    await t.pump(const Duration(milliseconds: 300));
    expect(reports, ['a1']);
    await t.pump(const Duration(seconds: 5));
    await t.pumpWidget(const SizedBox());
  });
}
