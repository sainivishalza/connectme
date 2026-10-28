import 'package:connectme/core/providers.dart';
import 'package:connectme/features/onboarding/welcome_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';

import 'helpers.dart';

Map<String, dynamic> me({required bool onboarded}) => {
      'ref': 'r', 'mode': 'open', 'display_name': 'Sam', 'onboarded': onboarded,
      'completeness': {'percent': 40, 'next_steps': ['add_photo']},
    };

Future<FakeAdapter> pump(WidgetTester t, bool onboarded) async {
  resetOnboardingGateForTest();
  final client = fakeClient((o) async {
    if (o.path == '/users/me') return Reply(200, me(onboarded: onboarded));
    if (o.path == '/users/me/onboarded') return Reply(204);
    return Reply(404);
  });
  final router = GoRouter(routes: [
    GoRoute(path: '/', builder: (_, _) => const OnboardingGate(child: Scaffold(body: Text('home', key: Key('home'))))),
    GoRoute(path: '/welcome', builder: (_, _) => const WelcomeScreen()),
    GoRoute(path: '/discover', builder: (_, _) => const Scaffold(body: Text('discover', key: Key('discover')))),
    GoRoute(path: '/profile/edit', builder: (_, _) => const Scaffold(body: Text('edit page'))),
  ]);
  await t.pumpWidget(ProviderScope(overrides: [apiClientProvider.overrideWithValue(client)], child: MaterialApp.router(routerConfig: router)));
  await t.pumpAndSettle();
  return client.dio.httpClientAdapter as FakeAdapter;
}

void main() {
  testWidgets('a new member is sent to the welcome; finishing marks it done and goes to discover', (t) async {
    final a = await pump(t, false);
    expect(find.byKey(const Key('welcome_title')), findsOneWidget);
    expect(find.text('Step 1 of 5'), findsOneWidget);
    expect(find.text('Your profile is 40% complete'), findsOneWidget);
    for (var i = 0; i < 4; i++) {
      await t.tap(find.byKey(const Key('welcome_next')));
      await t.pumpAndSettle();
    }
    expect(find.text('Step 5 of 5'), findsOneWidget);
    await t.tap(find.byKey(const Key('welcome_next'))); // "Start exploring"
    await t.pumpAndSettle();
    expect(a.count('/users/me/onboarded'), 1);
    expect(find.byKey(const Key('discover')), findsOneWidget);
  });

  testWidgets('skip for now also marks it done; a step opens its real screen then moves on', (t) async {
    final a = await pump(t, false);
    await t.tap(find.byKey(const Key('welcome_do')));
    await t.pumpAndSettle();
    expect(find.text('edit page'), findsOneWidget);
    t.state<NavigatorState>(find.byType(Navigator).last).pop();
    await t.pumpAndSettle();
    expect(find.text('Step 2 of 5'), findsOneWidget);
    await t.tap(find.byKey(const Key('skip_all')));
    await t.pumpAndSettle();
    expect(a.count('/users/me/onboarded'), 1);
  });

  testWidgets('an onboarded member never sees the welcome', (t) async {
    await pump(t, true);
    expect(find.byKey(const Key('home')), findsOneWidget);
    expect(find.byKey(const Key('welcome_title')), findsNothing);
  });
}
