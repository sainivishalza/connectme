import 'package:connectme/core/providers.dart';
import 'package:connectme/features/explore/explore_screen.dart';
import 'package:connectme/features/social/screens/feed_screen.dart';
import 'package:connectme/features/social/social_repository.dart';
import 'package:connectme/features/social/widgets/post_card.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';

import 'helpers.dart';

Map<String, dynamic> postJson(
  String id, {
  String kind = 'moment',
  String body = 'A post',
  String alias = '',
  Map<String, dynamic>? author,
  bool mine = false,
  int likes = 0,
  bool liked = false,
  int claims = 0,
  bool claimed = false,
  String hotel = '',
}) =>
    {
      'id': id,
      'kind': kind,
      'body': body,
      'alias': alias,
      'author': ?author,
      'distance': kind == 'missed' ? '' : 'within 2 km',
      'hotel_name': hotel,
      'created_at': DateTime.now().subtract(const Duration(minutes: 5)).toUtc().toIso8601String(),
      'mine': mine,
      'likes': likes,
      'liked': liked,
      'claims': claims,
      'claimed': claimed,
    };

Future<FakeAdapter> pumpFeed(WidgetTester t, PostKind kind, Handler h) async {
  final client = fakeClient(h);
  final router = GoRouter(initialLocation: '/feed', routes: [
    GoRoute(path: '/feed', builder: (_, _) => FeedScreen(kind: kind)),
    GoRoute(path: '/chats/:id', builder: (_, s) => Scaffold(body: Text('chat ${s.pathParameters['id']}', key: const Key('chat_page')))),
    GoRoute(path: '/hotel', builder: (_, _) => const Scaffold(body: Text('hotel'))),
    GoRoute(path: '/profile/edit', builder: (_, _) => const Scaffold(body: Text('edit'))),
  ]);
  await t.pumpWidget(ProviderScope(
    overrides: [apiClientProvider.overrideWithValue(client)],
    child: MaterialApp.router(routerConfig: router),
  ));
  await t.pumpAndSettle();
  return client.dio.httpClientAdapter as FakeAdapter;
}

void main() {
  test('timeAgo reads naturally', () {
    final now = DateTime(2026, 1, 1, 12);
    expect(timeAgo(now.subtract(const Duration(seconds: 20)), now: now), 'just now');
    expect(timeAgo(now.subtract(const Duration(minutes: 7)), now: now), '7 min ago');
    expect(timeAgo(now.subtract(const Duration(hours: 3)), now: now), '3 h ago');
    expect(timeAgo(now.subtract(const Duration(days: 2)), now: now), '2 d ago');
  });

  testWidgets('moments show authors the way the server allows, with distance and likes', (t) async {
    await pumpFeed(t, PostKind.moment, (o) async {
      if (o.path == '/social/feed') {
        return Reply(200, {
          'posts': [
            postJson('p1', body: 'Sunny walk anyone?', likes: 3, author: {'ref': 'r1', 'anonymous': false, 'mode': 'open', 'display_name': 'Olive'}),
            postJson('p2', body: 'Quiet café', author: {'ref': 'r2', 'anonymous': true, 'mode': 'incognito'}),
          ]
        });
      }
      return Reply(404, {'error': 'nope'});
    });
    expect(find.text('Olive'), findsOneWidget);
    expect(find.text('Anonymous'), findsOneWidget);
    expect(find.text('Sunny walk anyone?'), findsOneWidget);
    expect(find.textContaining('within 2 km'), findsNWidgets(2));
  });

  testWidgets('liking updates the count from the server', (t) async {
    await pumpFeed(t, PostKind.moment, (o) async {
      if (o.path == '/social/feed') return Reply(200, {'posts': [postJson('p1', likes: 1)]});
      if (o.path == '/social/posts/p1/like') return Reply(200, {'liked': true, 'likes': 2});
      return Reply(404, {'error': 'nope'});
    });
    expect(find.text('1'), findsOneWidget);
    await t.tap(find.byKey(const Key('post_like_p1')));
    await t.pumpAndSettle();
    expect(find.text('2'), findsOneWidget);
  });

  testWidgets('little universe shows only the nickname and no author', (t) async {
    await pumpFeed(t, PostKind.universe, (o) async {
      return Reply(200, {
        'posts': [postJson('u1', kind: 'universe', alias: 'Quiet Fox', body: 'Anyone else awake?')]
      });
    });
    expect(find.text('Quiet Fox'), findsOneWidget);
    expect(find.text('Anyone else awake?'), findsOneWidget);
  });

  testWidgets('composing posts the text and reloads the feed', (t) async {
    var posted = '';
    await pumpFeed(t, PostKind.universe, (o) async {
      if (o.path == '/social/feed') return Reply(200, {'posts': []});
      if (o.path == '/social/posts' && o.method == 'POST') {
        posted = (o.data as Map)['body'] as String;
        expect((o.data as Map)['kind'], 'universe');
        return Reply(201, postJson('n1', kind: 'universe', alias: 'Calm Owl', body: posted, mine: true));
      }
      return Reply(404, {'error': 'nope'});
    });
    expect(find.text('Quiet in the universe'), findsOneWidget);
    await t.tap(find.byKey(const Key('compose_post')));
    await t.pumpAndSettle();
    await t.enterText(find.byKey(const Key('post_body')), '  hello universe  ');
    await t.tap(find.byKey(const Key('post_send')));
    await t.pumpAndSettle();
    expect(posted, 'hello universe');
  });

  testWidgets('reporting a post sends the chosen reason', (t) async {
    final adapter = await pumpFeed(t, PostKind.moment, (o) async {
      if (o.path == '/social/feed') return Reply(200, {'posts': [postJson('p1')]});
      if (o.path == '/social/posts/p1/report') return Reply(204);
      return Reply(404, {'error': 'nope'});
    });
    await t.tap(find.byKey(const Key('post_menu_p1')));
    await t.pumpAndSettle();
    await t.tap(find.text('Report'));
    await t.pumpAndSettle();
    await t.tap(find.byKey(const Key('report_spam')));
    await t.pumpAndSettle();
    final req = adapter.requests.firstWhere((r) => r.path == '/social/posts/p1/report');
    expect((req.data as Map)['reason'], 'spam');
  });

  testWidgets('my own posts can be deleted, and have no report option', (t) async {
    final adapter = await pumpFeed(t, PostKind.moment, (o) async {
      if (o.path == '/social/feed') return Reply(200, {'posts': [postJson('p1', mine: true, body: 'Mine')]});
      if (o.path == '/social/posts/p1' && o.method == 'DELETE') return Reply(204);
      return Reply(404, {'error': 'nope'});
    });
    await t.tap(find.byKey(const Key('post_menu_p1')));
    await t.pumpAndSettle();
    expect(find.text('Report'), findsNothing);
    await t.tap(find.text('Delete'));
    await t.pumpAndSettle();
    expect(adapter.requests.any((r) => r.method == 'DELETE'), true);
    expect(find.text('Mine'), findsNothing);
  });

  testWidgets('missed connections: "That\'s me" is sent once and then disabled', (t) async {
    final adapter = await pumpFeed(t, PostKind.missed, (o) async {
      if (o.path == '/social/feed') return Reply(200, {'posts': [postJson('m1', kind: 'missed', hotel: 'Grand Hotel', body: 'Red scarf by the fireplace')]});
      if (o.path == '/social/posts/m1/claim') return Reply(204);
      return Reply(404, {'error': 'nope'});
    });
    expect(find.text('Someone at Grand Hotel'), findsOneWidget);
    await t.tap(find.byKey(const Key('post_claim_m1')));
    await t.pumpAndSettle();
    expect(adapter.requests.any((r) => r.path == '/social/posts/m1/claim'), true);
    expect(find.text('Sent'), findsOneWidget);
  });

  testWidgets('the author sees who said it was them and can accept into a chat', (t) async {
    await pumpFeed(t, PostKind.missed, (o) async {
      if (o.path == '/social/feed') return Reply(200, {'posts': [postJson('m1', kind: 'missed', mine: true, claims: 1, body: 'Mine')]});
      if (o.path == '/social/posts/m1/claims') {
        return Reply(200, {
          'claims': [
            {'id': 'c1', 'claimant': {'ref': 'r', 'anonymous': true, 'mode': 'hotel', 'intro': 'Coffee fan'}}
          ]
        });
      }
      if (o.path == '/social/claims/c1/accept') return Reply(200, {'match_id': 'match-7'});
      return Reply(404, {'error': 'nope'});
    });
    expect(find.text('1 said it was them'), findsOneWidget);
    await t.tap(find.byKey(const Key('post_claims_m1')));
    await t.pumpAndSettle();
    expect(find.text('Coffee fan'), findsOneWidget);
    expect(find.text('Anonymous'), findsWidgets);
    await t.tap(find.byKey(const Key('claim_accept_c1')));
    await t.pumpAndSettle();
    expect(find.text('chat match-7'), findsOneWidget);
  });

  testWidgets('a venue feed explains what to do when you are not checked in', (t) async {
    await pumpFeed(t, PostKind.missed, (o) async => Reply(422, {'error': 'check in at a venue first'}));
    expect(find.text('Check in at a venue first'), findsOneWidget);
    expect(find.text('Open Hotel mode'), findsOneWidget);
  });

  testWidgets('the explore hub links to every feed', (t) async {
    final router = GoRouter(initialLocation: '/explore', routes: [
      GoRoute(path: '/explore', builder: (_, _) => const ExploreScreen()),
      GoRoute(path: '/feed/:kind', builder: (_, s) => Scaffold(body: Text('feed ${s.pathParameters['kind']}'))),
      GoRoute(path: '/random', builder: (_, _) => const Scaffold(body: Text('random'))),
      GoRoute(path: '/live', builder: (_, _) => const Scaffold(body: Text('live'))),
    ]);
    await t.binding.setSurfaceSize(const Size(500, 3200));
    addTearDown(() => t.binding.setSurfaceSize(null));
    await t.pumpWidget(MaterialApp.router(routerConfig: router));
    await t.pumpAndSettle();
    for (final id in ['moments', 'universe', 'missed', 'groups', 'random', 'masked', 'videos', 'audio', 'forums', 'live']) {
      expect(find.byKey(Key('explore_$id')), findsOneWidget);
    }
    await t.tap(find.byKey(const Key('explore_universe')));
    await t.pumpAndSettle();
    expect(find.text('feed universe'), findsOneWidget);
  });
}
