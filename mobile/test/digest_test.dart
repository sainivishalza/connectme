import 'package:connectme/core/providers.dart';
import 'package:connectme/features/digest/unsubscribe_screen.dart';
import 'package:connectme/features/push/screens/notifications_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

void main() {
  testWidgets('the weekly summary switch loads its state and saves a change', (t) async {
    t.view.physicalSize = const Size(800, 2600);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.reset);
    var on = false;
    final client = fakeClient((o) async {
      if (o.path == '/push/prefs') return Reply(200, {'prefs': {'messages': true}, 'devices': 0});
      if (o.path == '/digest/me' && o.method == 'GET') return Reply(200, {'enabled': on});
      if (o.path == '/digest/me' && o.method == 'PUT') {
        on = (o.data as Map)['enabled'] as bool;
        return Reply(204);
      }
      return Reply(404);
    });
    await t.pumpWidget(ProviderScope(overrides: [apiClientProvider.overrideWithValue(client)], child: const MaterialApp(home: NotificationsScreen())));
    await t.pumpAndSettle();
    expect(t.widget<SwitchListTile>(find.byKey(const Key('pref_digest'))).value, isFalse);
    await t.tap(find.byKey(const Key('pref_digest')));
    await t.pumpAndSettle();
    expect(on, isTrue);
    expect(t.widget<SwitchListTile>(find.byKey(const Key('pref_digest'))).value, isTrue);
  });

  testWidgets('the email link unsubscribes in one tap, and a bad link says so', (t) async {
    final posted = <Object?>[];
    final client = fakeClient((o) async {
      if (o.path == '/digest/unsubscribe') {
        posted.add(o.data);
        return (o.data as Map)['token'] == 'good' ? Reply(204) : Reply(404, {'error': 'not found'});
      }
      return Reply(404);
    });
    Future<void> open(String token) async {
      await t.pumpWidget(ProviderScope(overrides: [apiClientProvider.overrideWithValue(client)], child: MaterialApp(home: DigestUnsubscribeScreen(key: ValueKey(token), token: token))));
      await t.pumpAndSettle();
      await t.tap(find.byKey(const Key('unsub_go')));
      await t.pumpAndSettle();
    }

    await open('good');
    expect(find.byKey(const Key('unsub_done')), findsOneWidget);
    await open('bad');
    expect(find.byKey(const Key('unsub_failed')), findsOneWidget);
    expect(posted, [
      {'token': 'good'},
      {'token': 'bad'}
    ]);
  });
}
