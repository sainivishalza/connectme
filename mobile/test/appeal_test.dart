import 'package:connectme/core/providers.dart';
import 'package:connectme/core/router/app_router.dart';
import 'package:connectme/features/auth/auth_controller.dart';
import 'package:connectme/features/auth/screens/appeal_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

void main() {
  test('the appeal page opens for people who are signed out', () {
    for (final s in [AuthStatus.unknown, AuthStatus.signedOut]) {
      expect(redirectFor(s, '/appeal'), isNull);
    }
  });

  testWidgets('the form needs an email, a password and a real message, then says it was sent', (t) async {
    t.view.physicalSize = const Size(800, 1800);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.reset);
    final client = fakeClient((o) async => o.path == '/moderation/appeals' ? Reply(202, {'status': 'received'}) : Reply(404));
    await t.pumpWidget(ProviderScope(overrides: [apiClientProvider.overrideWithValue(client)], child: const MaterialApp(home: AppealScreen())));
    expect(t.widget<FilledButton>(find.byKey(const Key('appeal_send'))).onPressed, isNull);
    await t.enterText(find.byKey(const Key('appeal_email')), 'me@example.com');
    await t.enterText(find.byKey(const Key('appeal_password')), 'secret');
    await t.enterText(find.byKey(const Key('appeal_message')), 'short');
    await t.pump();
    expect(t.widget<FilledButton>(find.byKey(const Key('appeal_send'))).onPressed, isNull);
    await t.enterText(find.byKey(const Key('appeal_message')), 'Please look again, I was reported by mistake.');
    await t.pump();
    await t.tap(find.byKey(const Key('appeal_send')));
    await t.pumpAndSettle();
    final a = client.dio.httpClientAdapter as FakeAdapter;
    expect(a.requests.single.data, {'email': 'me@example.com', 'password': 'secret', 'message': 'Please look again, I was reported by mistake.'});
    expect(find.byKey(const Key('appeal_sent')), findsOneWidget);
  });

  testWidgets('a refused appeal keeps the form so it can be corrected', (t) async {
    t.view.physicalSize = const Size(800, 1800);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.reset);
    final client = fakeClient((o) async => Reply(403, {'error': 'we could not send this appeal'}));
    await t.pumpWidget(ProviderScope(overrides: [apiClientProvider.overrideWithValue(client)], child: const MaterialApp(home: Scaffold(body: AppealScreen()))));
    await t.enterText(find.byKey(const Key('appeal_email')), 'me@example.com');
    await t.enterText(find.byKey(const Key('appeal_password')), 'wrong');
    await t.enterText(find.byKey(const Key('appeal_message')), 'Please look again, I was reported by mistake.');
    await t.pump();
    await t.tap(find.byKey(const Key('appeal_send')));
    await t.pumpAndSettle();
    expect(find.byKey(const Key('appeal_sent')), findsNothing);
    expect(find.byKey(const Key('appeal_send')), findsOneWidget);
  });
}
