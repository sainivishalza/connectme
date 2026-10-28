import 'package:connectme/core/providers.dart';
import 'package:connectme/features/audio/screens/audio_list_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

void main() {
  testWidgets('voice room list filters by topic and shows each room topic', (t) async {
    t.view.physicalSize = const Size(2400, 1200);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.reset);
    final client = fakeClient((o) async {
      if (o.path == '/audio/rooms') {
        final topic = o.queryParameters['topic'];
        final all = [
          {'id': 'a', 'title': 'Late night talk', 'anonymous': true, 'topic': 'nightowls', 'speakers': 1, 'listeners': 4},
          {'id': 'b', 'title': 'Open mic', 'anonymous': false, 'topic': 'open', 'speakers': 2, 'listeners': 0},
        ];
        return Reply(200, {'rooms': all.where((r) => topic == null || r['topic'] == topic).toList()});
      }
      return Reply(404);
    });
    await t.pumpWidget(ProviderScope(overrides: [apiClientProvider.overrideWithValue(client)], child: const MaterialApp(home: AudioListScreen())));
    await t.pumpAndSettle();
    expect(find.text('Late night talk'), findsOneWidget);
    expect(find.textContaining('Night owls'), findsWidgets);
    await t.tap(find.byKey(const Key('audio_topic_nightowls')));
    await t.pumpAndSettle();
    expect((client.dio.httpClientAdapter as FakeAdapter).requests.last.queryParameters['topic'], 'nightowls');
    expect(find.byKey(const Key('audio_b')), findsNothing);
  });
}
