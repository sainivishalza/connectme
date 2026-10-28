import 'package:connectme/core/providers.dart';
import 'package:connectme/features/games/screens/most_likely_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

void main() {
  testWidgets('start a round, pick someone, see totals but not who voted, then go on to the next prompt', (t) async {
    t.view.physicalSize = const Size(800, 2000);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.reset);
    var voted = false;
    var started = false;
    final client = fakeClient((o) async {
      Map<String, dynamic> state() => {
            'active': true,
            'poll_id': 'p1',
            'prompt': 'Most likely to arrive early',
            'closed': false,
            'members': 4,
            'voted': voted ? 1 : 0,
            if (voted) 'my_vote': 'r1',
            'candidates': [
              {'id': 'r1', 'label': 'Mina', if (voted) 'count': 1},
              {'id': 'r2', 'label': 'Member A', if (voted) 'count': 0},
            ],
          };
      if (o.path == '/games/group/g1/mlt' && o.method == 'GET') return Reply(200, started ? state() : {'active': false, 'members': 4});
      if (o.path == '/games/group/g1/mlt/start') {
        started = true;
        return Reply(200, state());
      }
      if (o.path == '/games/group/g1/mlt/vote') {
        voted = true;
        return Reply(200, state());
      }
      return Reply(404);
    });
    await t.pumpWidget(ProviderScope(
      overrides: [apiClientProvider.overrideWithValue(client)],
      child: const MaterialApp(home: MostLikelyScreen(groupMatchId: 'g1', pollInterval: Duration(seconds: 30))),
    ));
    await t.pumpAndSettle();
    await t.tap(find.byKey(const Key('mlt_start')));
    await t.pumpAndSettle();
    expect(find.byKey(const Key('mlt_prompt')), findsOneWidget);
    expect(find.text('0 of 4 have voted'), findsOneWidget);
    expect(find.textContaining('vote'), findsWidgets);
    expect(find.text('1 vote - your pick'), findsNothing);

    await t.tap(find.byKey(const Key('mlt_r1')));
    await t.pumpAndSettle();
    expect(find.text('1 vote - your pick'), findsOneWidget);
    expect(find.text('0 votes'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
  });
}
