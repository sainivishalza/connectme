import 'package:connectme/core/providers.dart';
import 'package:connectme/features/social/screens/feed_screen.dart';
import 'package:connectme/features/social/social_repository.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';

import 'helpers.dart';
import 'social_test.dart' show postJson;

Future<void> settle(WidgetTester t) async {
  await t.pumpAndSettle();
  await t.pump(const Duration(milliseconds: 100));
  await t.pumpAndSettle();
}

void main() {
  testWidgets('comments open from a post, show nicknames, and a new comment is added and counted', (t) async {
    final comments = <Map<String, dynamic>>[
      {'id': 'c1', 'body': 'Honestly, same', 'name': 'Quiet Fox', 'mine': false, 'author': false},
      {'id': 'c2', 'body': 'Thanks all', 'name': 'Calm Owl', 'mine': false, 'author': true},
    ];
    final client = fakeClient((o) async {
      if (o.path == '/social/feed') {
        return Reply(200, {
          'posts': [
            {...postJson('p1', kind: 'universe', alias: 'Calm Owl', body: 'A late night thought'), 'comments': comments.length}
          ]
        });
      }
      if (o.path == '/social/posts/p1/comments' && o.method == 'GET') return Reply(200, {'comments': comments});
      if (o.path == '/social/posts/p1/comments' && o.method == 'POST') {
        final c = {'id': 'c3', 'body': (o.data as Map)['body'], 'name': 'Sunny Wren', 'mine': true, 'author': false};
        comments.add(c);
        return Reply(201, c);
      }
      return Reply(404);
    });
    final router = GoRouter(initialLocation: '/feed', routes: [GoRoute(path: '/feed', builder: (_, _) => const FeedScreen(kind: PostKind.universe))]);
    await t.pumpWidget(ProviderScope(overrides: [apiClientProvider.overrideWithValue(client)], child: MaterialApp.router(routerConfig: router)));
    await settle(t);

    expect(find.text('2'), findsOneWidget); // comment count on the card
    await t.tap(find.byKey(const Key('post_comments_p1')));
    await settle(t);
    expect(find.text('Quiet Fox'), findsOneWidget);
    expect(find.text('Author'), findsOneWidget);

    // Too short to send; then a real one.
    await t.enterText(find.byKey(const Key('comment_text')), 'ok');
    await t.pump();
    expect(t.widget<IconButton>(find.byKey(const Key('send_comment'))).onPressed, isNull);
    await t.enterText(find.byKey(const Key('comment_text')), 'Me too, honestly');
    await t.pump();
    await t.tap(find.byKey(const Key('send_comment')));
    await settle(t);
    expect(find.text('Me too, honestly'), findsOneWidget);
    expect(find.text('You'), findsOneWidget);
    final adapter = client.dio.httpClientAdapter as FakeAdapter;
    expect(adapter.requests.where((r) => r.method == 'POST' && r.path == '/social/posts/p1/comments').length, 1);
  });

  testWidgets('post authors can delete any comment; others can only report', (t) async {
    final client = fakeClient((o) async {
      if (o.path == '/social/feed') {
        return Reply(200, {
          'posts': [postJson('p2', mine: true, body: 'My own post')]
        });
      }
      if (o.path == '/social/posts/p2/comments') {
        return Reply(200, {
          'comments': [
            {'id': 'x1', 'body': 'rude comment', 'name': 'Someone', 'mine': false}
          ]
        });
      }
      if (o.path == '/social/comments/x1') return Reply(204);
      return Reply(404);
    });
    final router = GoRouter(initialLocation: '/feed', routes: [GoRoute(path: '/feed', builder: (_, _) => const FeedScreen(kind: PostKind.moment))]);
    await t.pumpWidget(ProviderScope(overrides: [apiClientProvider.overrideWithValue(client)], child: MaterialApp.router(routerConfig: router)));
    await settle(t);
    await t.tap(find.byKey(const Key('post_comments_p2')));
    await settle(t);
    expect(find.byKey(const Key('report_comment_x1')), findsNothing);
    await t.tap(find.byKey(const Key('delete_comment_x1')));
    await settle(t);
    expect((client.dio.httpClientAdapter as FakeAdapter).requests.any((r) => r.method == 'DELETE' && r.path == '/social/comments/x1'), isTrue);
  });
}
