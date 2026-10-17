import 'package:connectme/core/providers.dart';
import 'package:connectme/features/random_chat/screens/random_screen.dart';
import 'package:connectme/features/random_chat/widgets/random_banner.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';

import 'helpers.dart';

Map<String, dynamic> matched(String id) => {
      'state': 'matched',
      'session': {
        'match_id': id,
        'expires_at': DateTime.now().add(const Duration(minutes: 9)).toUtc().toIso8601String(),
        'kept_by_me': false,
        'kept': false,
        'peer': {'ref': 'peer-ref', 'anonymous': true, 'mode': 'incognito', 'tags': ['jazz']},
      },
    };

class _Queue {
  bool waiting = false;
  int polls = 0;
}

Future<(FakeAdapter, GoRouter)> pumpRandom(WidgetTester t, Handler h) async {
  final client = fakeClient(h);
  final adapter = client.dio.httpClientAdapter as FakeAdapter;
  final router = GoRouter(initialLocation: '/random', routes: [
    GoRoute(path: '/random', builder: (_, _) => const RandomScreen(pollInterval: Duration(milliseconds: 100))),
    GoRoute(path: '/chats/:id', builder: (_, s) => Scaffold(body: Text('chat ${s.pathParameters['id']}', key: const Key('chat_page')))),
  ]);
  await t.pumpWidget(ProviderScope(
    overrides: [apiClientProvider.overrideWithValue(client)],
    child: MaterialApp.router(routerConfig: router),
  ));
  await t.pump();
  await t.pump(const Duration(milliseconds: 50));
  return (adapter, router);
}

void main() {
  testWidgets('start joins the queue, shows searching, and opens the chat when paired', (t) async {
    final q = _Queue();
    final (adapter, _) = await pumpRandom(t, (o) async {
      if (o.path == '/random/status') {
        if (!q.waiting) return Reply(200, {'state': 'idle'});
        q.polls++;
        return Reply(200, q.polls < 3 ? {'state': 'waiting'} : matched('m1'));
      }
      if (o.path == '/random/join') {
        q.waiting = true;
        return Reply(200, {'state': 'waiting'});
      }
      return Reply(404, {'error': 'nope'});
    });
    expect(find.byKey(const Key('random_start')), findsOneWidget);

    await t.tap(find.byKey(const Key('random_start')));
    await t.pump(const Duration(milliseconds: 20));
    expect(find.text('Looking for someone'), findsOneWidget);
    expect(find.byKey(const Key('random_cancel')), findsOneWidget);

    await t.pump(const Duration(milliseconds: 450));
    await t.pumpAndSettle();
    expect(find.byKey(const Key('chat_page')), findsOneWidget, reason: 'pairing opens the chat');
    expect(find.text('chat m1'), findsOneWidget);
    expect(adapter.requests.any((r) => r.path == '/random/join'), true);
  });

  testWidgets('stop looking leaves the queue', (t) async {
    final q = _Queue();
    final (adapter, _) = await pumpRandom(t, (o) async {
      if (o.path == '/random/status') return Reply(200, {'state': q.waiting ? 'waiting' : 'idle'});
      if (o.path == '/random/join') {
        q.waiting = true;
        return Reply(200, {'state': 'waiting'});
      }
      if (o.path == '/random/cancel') {
        q.waiting = false;
        return Reply(204);
      }
      return Reply(404, {'error': 'nope'});
    });
    await t.tap(find.byKey(const Key('random_start')));
    await t.pump(const Duration(milliseconds: 20));
    await t.tap(find.byKey(const Key('random_cancel')));
    await t.pump(const Duration(milliseconds: 50));
    expect(adapter.requests.any((r) => r.path == '/random/cancel'), true);
    expect(find.byKey(const Key('random_start')), findsOneWidget);
  });

  testWidgets('an already running random chat is resumed instead of showing the start button', (t) async {
    await pumpRandom(t, (o) async {
      if (o.path == '/random/status') return Reply(200, matched('m9'));
      return Reply(404, {'error': 'nope'});
    });
    await t.pumpAndSettle();
    expect(find.text('chat m9'), findsOneWidget);
  });

  testWidgets('banner counts down, offers Keep chatting, and then waits for the other person', (t) async {
    final client = fakeClient((o) async {
      if (o.path == '/random/m1/keep') return Reply(200, matched('m1')['session'] as Map<String, dynamic>);
      return Reply(404, {'error': 'nope'});
    });
    final adapter = client.dio.httpClientAdapter as FakeAdapter;
    final start = DateTime(2026, 1, 1, 12);
    var clock = start;
    var ended = 0;
    await t.pumpWidget(ProviderScope(
      overrides: [apiClientProvider.overrideWithValue(client)],
      child: MaterialApp(
        home: Scaffold(
          body: RandomBanner(
            convId: 'm1',
            expiresAt: start.add(const Duration(minutes: 10)),
            keptByMe: false,
            now: () => clock,
            onEnded: () => ended++,
          ),
        ),
      ),
    ));
    expect(find.text('Anonymous chat ends in 10:00'), findsOneWidget);
    clock = start.add(const Duration(minutes: 1, seconds: 5));
    await t.pump(const Duration(seconds: 1));
    expect(find.text('Anonymous chat ends in 08:55'), findsOneWidget);
    expect(find.text('It stays only if you both keep chatting.'), findsOneWidget);

    await t.tap(find.byKey(const Key('random_keep')));
    await t.pump(const Duration(milliseconds: 50));
    expect(adapter.requests.any((r) => r.path == '/random/m1/keep' && r.method == 'POST'), true);
    expect(ended, 0);
    await t.pumpWidget(const SizedBox());
  });

  testWidgets('banner tells the person it ended when time runs out', (t) async {
    final start = DateTime(2026, 1, 1, 12);
    var clock = start;
    var ended = 0;
    await t.pumpWidget(ProviderScope(
      child: MaterialApp(
        home: Scaffold(
          body: RandomBanner(
            convId: 'm1',
            expiresAt: start.add(const Duration(seconds: 2)),
            keptByMe: true,
            now: () => clock,
            onEnded: () => ended++,
          ),
        ),
      ),
    ));
    expect(find.text('Waiting for them to keep it too.'), findsOneWidget);
    expect(find.byKey(const Key('random_keep')), findsNothing, reason: 'already kept by me');
    clock = start.add(const Duration(seconds: 3));
    await t.pump(const Duration(seconds: 1));
    expect(ended, 1);
    await t.pump(const Duration(seconds: 5)); // snackbar timer
    await t.pumpWidget(const SizedBox());
  });

  testWidgets('Masked Truth asks for the truth queue and opens the chat with the game', (t) async {
    final client = fakeClient((o) async {
      if (o.path == '/random/status') return Reply(200, {'state': 'idle'});
      if (o.path == '/random/join') {
        final m = matched('m7');
        (m['session'] as Map)['style'] = 'truth';
        return Reply(200, m);
      }
      return Reply(404, {'error': 'nope'});
    });
    final adapter = client.dio.httpClientAdapter as FakeAdapter;
    final router = GoRouter(initialLocation: '/masked', routes: [
      GoRoute(path: '/masked', builder: (_, _) => const RandomScreen(style: 'truth', pollInterval: Duration(milliseconds: 100))),
      GoRoute(
          path: '/chats/:id',
          builder: (_, s) => Scaffold(body: Text('chat ${s.pathParameters['id']} game=${s.uri.queryParameters['game']}', key: const Key('chat_page')))),
    ]);
    await t.pumpWidget(ProviderScope(
      overrides: [apiClientProvider.overrideWithValue(client)],
      child: MaterialApp.router(routerConfig: router),
    ));
    await t.pump();
    expect(find.text('Masked Truth'), findsOneWidget);
    expect(find.text('Find a player'), findsOneWidget);
    await t.tap(find.byKey(const Key('random_start')));
    await t.pumpAndSettle();
    final join = adapter.requests.firstWhere((r) => r.path == '/random/join');
    expect((join.data as Map)['style'], 'truth');
    expect(find.text('chat m7 game=1'), findsOneWidget);
  });
}
