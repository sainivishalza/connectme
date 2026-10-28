import 'package:connectme/core/providers.dart';
import 'package:connectme/features/partners/screens/event_group_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

void main() {
  testWidgets('the board shows who is going and messages, sends one, and deletes own / reports others', (t) async {
    t.view.physicalSize = const Size(800, 2000);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.reset);
    final messages = <Map<String, dynamic>>[
      {'id': 'm1', 'body': 'Who is coming early?', 'name': 'Mina', 'mine': false},
      {'id': 'm2', 'body': 'I might be', 'name': 'Someone', 'mine': false},
    ];
    final calls = <String>[];
    final client = fakeClient((o) async {
      if (o.path == '/eventgroups/e1' && o.method == 'GET') {
        return Reply(200, {'title': 'Rooftop mixer', 'going': 5, 'members': [{'name': 'Mina'}, {'name': 'Leo'}], 'messages': messages});
      }
      if (o.path == '/eventgroups/e1/messages') {
        calls.add('post ${(o.data as Map)['body']}');
        messages.add({'id': 'm3', 'body': (o.data as Map)['body'], 'name': 'Me', 'mine': true});
        return Reply(204);
      }
      if (o.path == '/eventgroups/messages/m3' && o.method == 'DELETE') {
        calls.add('delete m3');
        messages.removeWhere((m) => m['id'] == 'm3');
        return Reply(204);
      }
      if (o.path == '/eventgroups/messages/m1/report') {
        calls.add('report m1');
        return Reply(204);
      }
      return Reply(404);
    });
    await t.pumpWidget(ProviderScope(overrides: [apiClientProvider.overrideWithValue(client)], child: const MaterialApp(home: EventGroupScreen(eventId: 'e1'))));
    await t.pumpAndSettle();
    expect(find.text('Rooftop mixer'), findsOneWidget);
    expect(find.text('5 going: Mina, Leo'), findsOneWidget);
    expect(find.text('Who is coming early?'), findsOneWidget);

    await t.enterText(find.byKey(const Key('event_text')), 'See you there');
    await t.tap(find.byKey(const Key('event_send')));
    await t.pumpAndSettle();
    expect(find.text('See you there'), findsOneWidget);
    await t.tap(find.byKey(const Key('delete_m3')));
    await t.pumpAndSettle();
    expect(find.text('See you there'), findsNothing);
    await t.tap(find.byKey(const Key('report_m1')));
    await t.pumpAndSettle();
    expect(calls, ['post See you there', 'delete m3', 'report m1']);
  });

  testWidgets('someone without a spot sees an error, not the board', (t) async {
    final client = fakeClient((o) async => Reply(404, {'error': 'not found'}));
    await t.pumpWidget(ProviderScope(overrides: [apiClientProvider.overrideWithValue(client)], child: const MaterialApp(home: EventGroupScreen(eventId: 'e9'))));
    await t.pumpAndSettle();
    expect(find.text('Could not load'), findsOneWidget);
    expect(find.byKey(const Key('event_text')), findsNothing);
  });
}
