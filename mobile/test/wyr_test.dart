import 'package:connectme/core/providers.dart';
import 'package:connectme/features/games/screens/would_you_rather_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

Map<String, dynamic> round({String my = '', int a = 0, int b = 0, bool closed = false, int voted = 0}) => {
      'active': true,
      'poll_id': 'p1',
      'a': 'Live by the sea',
      'b': 'Live in the mountains',
      'closed': closed,
      'members': 4,
      'voted': voted,
      if (my.isNotEmpty) 'my_choice': my,
      if (my.isNotEmpty || closed) 'count_a': a,
      if (my.isNotEmpty || closed) 'count_b': b,
    };

void main() {
  testWidgets('start a round, vote once, see anonymous totals, then move on to the next question', (t) async {
    t.view.physicalSize = const Size(800, 1600);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.reset);
    var phase = 'idle';
    final votes = <String>[];
    final client = fakeClient((o) async {
      if (o.path == '/games/group/g1' && o.method == 'GET') {
        return Reply(200, switch (phase) {
          'idle' => {'active': false, 'closed': false, 'members': 4, 'voted': 0},
          'open' => round(voted: 1),
          'voted' => round(my: 'b', a: 0, b: 1, voted: 1),
          _ => round(my: 'b', a: 2, b: 2, closed: true, voted: 4),
        });
      }
      if (o.path == '/games/group/g1/start') {
        phase = 'open';
        return Reply(200, round());
      }
      if (o.path == '/games/group/g1/vote') {
        votes.add((o.data as Map)['choice'] as String);
        phase = 'voted';
        return Reply(200, round(my: 'b', a: 0, b: 1, voted: 1));
      }
      return Reply(404);
    });
    await t.pumpWidget(ProviderScope(
      overrides: [apiClientProvider.overrideWithValue(client)],
      child: const MaterialApp(home: WouldYouRatherScreen(groupMatchId: 'g1', pollInterval: Duration(seconds: 1))),
    ));
    await t.pumpAndSettle();
    expect(find.byKey(const Key('wyr_start')), findsOneWidget);

    await t.tap(find.byKey(const Key('wyr_start')));
    await t.pumpAndSettle();
    expect(find.text('Live by the sea'), findsOneWidget);
    expect(find.byKey(const Key('wyr_count_a')), findsNothing); // no totals before voting

    await t.tap(find.byKey(const Key('wyr_b')));
    await t.pumpAndSettle();
    expect(votes, ['b']);
    expect(find.textContaining('your pick'), findsOneWidget);
    // Voting again does nothing: the card is no longer tappable.
    await t.tap(find.byKey(const Key('wyr_a')));
    await t.pumpAndSettle();
    expect(votes, ['b']);

    // The poll timer brings in everyone else's votes and the finished round.
    phase = 'closed';
    await t.pump(const Duration(seconds: 2));
    await t.pumpAndSettle();
    expect(find.text('Round finished'), findsOneWidget);
    expect(find.textContaining('2 votes'), findsNWidgets(2));
    expect(find.byKey(const Key('wyr_next')), findsOneWidget);
    await t.pumpWidget(const SizedBox()); // stop the poll timer
  });
}
