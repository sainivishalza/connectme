import 'dart:convert';

import 'package:connectme/core/network/token_store.dart';
import 'package:connectme/core/providers.dart';
import 'package:connectme/features/auth/auth_controller.dart';
import 'package:connectme/features/auth/screens/login_screen.dart';
import 'package:connectme/features/discovery/screens/discover_screen.dart';
import 'package:dio/dio.dart' show RequestOptions;
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

Map<String, dynamic> person(String ref, String name, {bool anon = false}) => {
      'ref': ref,
      'anonymous': anon,
      'mode': anon ? 'incognito' : 'open',
      if (!anon) 'display_name': name,
      'tags': ['hiking'],
      'intro': 'hi from $name',
      'distance': 'within 5 km',
    };

String fakeJwt(String sub) =>
    'eyJhbGciOiJIUzI1NiJ9.${base64Url.encode(utf8.encode('{"sub":"$sub"}')).replaceAll('=', '')}.sig';

void main() {
  group('login', () {
    Future<(ProviderContainer, FakeAdapter)> pumpLogin(WidgetTester t, Handler h, MemoryTokenStore tokens) async {
      late FakeAdapter adapter;
      final client = fakeClient((o) async => h(o), tokens: tokens);
      adapter = client.dio.httpClientAdapter as FakeAdapter;
      final container = ProviderContainer(overrides: [
        apiClientProvider.overrideWithValue(client),
        tokenStoreProvider.overrideWithValue(tokens),
      ]);
      addTearDown(container.dispose);
      await t.pumpWidget(UncontrolledProviderScope(
        container: container,
        child: const MaterialApp(home: LoginScreen()),
      ));
      return (container, adapter);
    }

    testWidgets('validates before calling the server', (t) async {
      final (_, adapter) = await pumpLogin(t, (o) async => Reply(200), MemoryTokenStore());
      await t.tap(find.byKey(const Key('login')));
      await t.pump();
      expect(find.text('Enter your email'), findsOneWidget);
      expect(find.text('Password is required'), findsOneWidget);
      expect(adapter.requests, isEmpty);
    });

    testWidgets('success stores the session and signs in', (t) async {
      final tokens = MemoryTokenStore();
      final (c, adapter) = await pumpLogin(
          t,
          (o) async => Reply(200, {
                'access_token': fakeJwt('user-7'),
                'refresh_token': 'R',
                'email_verified': true,
              }),
          tokens);
      await t.enterText(find.byKey(const Key('email')), ' me@example.com ');
      await t.enterText(find.byKey(const Key('password')), 'Correct-Horse-9!');
      await t.tap(find.byKey(const Key('login')));
      await t.pumpAndSettle();

      final body = adapter.requests.single.data as Map;
      expect(body['email'], 'me@example.com', reason: 'email is trimmed');
      expect((await tokens.read())!.refresh, 'R');
      final s = c.read(authControllerProvider);
      expect(s.status, AuthStatus.signedIn);
      expect(s.userId, 'user-7');
    });

    testWidgets('wrong credentials show the server message and keep the user signed out', (t) async {
      final tokens = MemoryTokenStore();
      final (c, _) = await pumpLogin(t, (o) async => Reply(401, {'error': 'invalid credentials or token'}), tokens);
      await t.enterText(find.byKey(const Key('email')), 'me@example.com');
      await t.enterText(find.byKey(const Key('password')), 'nope');
      await t.tap(find.byKey(const Key('login')));
      await t.pumpAndSettle();
      expect(find.text('invalid credentials or token'), findsOneWidget);
      expect(await tokens.read(), isNull);
      expect(c.read(authControllerProvider).status, isNot(AuthStatus.signedIn));
    });
  });

  group('swipe deck', () {
    Future<(List<String>, FakeAdapter)> pumpDeck(WidgetTester t, Handler h) async {
      final client = fakeClient(h, tokens: MemoryTokenStore());
      final adapter = client.dio.httpClientAdapter as FakeAdapter;
      await t.pumpWidget(ProviderScope(
        overrides: [apiClientProvider.overrideWithValue(client)],
        child: const MaterialApp(home: Scaffold(body: SwipeDeck())),
      ));
      await t.pumpAndSettle();
      return (<String>[], adapter);
    }

    Handler nearby(List<Map<String, dynamic>> people, Future<Reply> Function(RequestOptions) swipe) => (o) async {
          if (o.path == '/users/nearby') return Reply(200, {'results': people});
          if (o.path.startsWith('/matching/')) return swipe(o);
          return Reply(404, {'error': 'nope'});
        };

    testWidgets('likes advance the deck; a match opens the dialog', (t) async {
      var n = 0;
      final (_, adapter) = await pumpDeck(
        t,
        nearby([person('a', 'Ana'), person('b', 'Bea')], (o) async {
          n++;
          return n == 1 ? Reply(200, {'matched': false}) : Reply(200, {'matched': true, 'match_id': 'm1'});
        }),
      );
      expect(find.text('Ana'), findsOneWidget);
      await t.tap(find.byKey(const Key('like')));
      await t.pumpAndSettle();
      expect(adapter.requests.last.data, {'target_ref': 'a', 'action': 'like'});
      expect(find.text('Bea'), findsOneWidget);
      expect(find.text('Ana'), findsNothing);

      await t.tap(find.byKey(const Key('like')));
      await t.pumpAndSettle();
      expect(find.text("It's a match!"), findsOneWidget);
    });

    testWidgets('pass sends a pass and an anonymous card shows no name', (t) async {
      final (_, adapter) = await pumpDeck(t, nearby([person('x', '', anon: true)], (o) async => Reply(200, {'matched': false})));
      expect(find.text('Anonymous'), findsNWidgets(2));
      await t.tap(find.byKey(const Key('pass')));
      await t.pumpAndSettle();
      expect(adapter.requests.last.data, {'target_ref': 'x', 'action': 'pass'});
      expect(find.text("You're all caught up"), findsOneWidget);
    });

    testWidgets('super like without coins offers the upgrade instead of failing silently', (t) async {
      await pumpDeck(t, nearby([person('a', 'Ana')], (o) async => Reply(402, {'error': 'not enough coins'})));
      await t.tap(find.byKey(const Key('superlike')));
      await t.pumpAndSettle();
      expect(find.text('not enough coins'), findsOneWidget);
      expect(find.text('See plans'), findsOneWidget);
      expect(find.text('Ana'), findsOneWidget, reason: 'the card must stay when the action failed');
    });

    testWidgets('missing location prompts to share it', (t) async {
      await pumpDeck(t, (o) async => Reply(403, {'error': 'set your location first'}));
      expect(find.text('Share your location'), findsOneWidget);
      expect(find.byKey(const Key('share_location')), findsOneWidget);
    });

    testWidgets('an unverified account is told to verify email', (t) async {
      await pumpDeck(t, (o) async => Reply(403, {'error': 'verify your email first'}));
      expect(find.text('Verify your email'), findsOneWidget);
    });
  });
}
