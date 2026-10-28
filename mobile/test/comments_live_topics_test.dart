import 'package:connectme/core/providers.dart';
import 'package:connectme/features/live/media_session.dart';
import 'package:connectme/features/live/screens/broadcast_screen.dart';
import 'package:connectme/features/live/screens/live_list_screen.dart';
import 'package:connectme/features/live/screens/live_room_screen.dart';
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

class _Media implements LiveMedia {
  @override
  Future<void> connect(String url, String token, {required bool publish}) async {}
  @override
  Widget view() => const ColoredBox(color: Colors.black);
  @override
  Future<void> disconnect() async {}
}

Future<FakeAdapter> pumpWith(WidgetTester t, Widget home, Handler h) async {
  t.view.physicalSize = const Size(800, 1800);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.reset);
  final client = fakeClient(h);
  await t.pumpWidget(ProviderScope(
    overrides: [apiClientProvider.overrideWithValue(client), liveMediaFactoryProvider.overrideWithValue(() => _Media())],
    child: MaterialApp(home: home),
  ));
  await settle(t);
  return client.dio.httpClientAdapter as FakeAdapter;
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

  testWidgets('live list filters by topic and by people you follow, and shows pinned lines', (t) async {
    final a = await pumpWith(t, const LiveListScreen(), (o) async {
      if (o.path == '/live/rooms') {
        final topic = o.queryParameters['topic'];
        final all = [
          {'id': 'a', 'title': 'Acoustic', 'host_ref': 'h1', 'host_name': 'Mia', 'mine': false, 'topic': 'music', 'pinned': 'Requests open', 'following': true},
          {'id': 'b', 'title': 'Chill chat', 'host_ref': 'h2', 'host_name': 'Leo', 'mine': false, 'topic': 'chat'},
        ];
        return Reply(200, {'rooms': all.where((r) => topic == null || r['topic'] == topic).toList()});
      }
      return Reply(404);
    });
    expect(find.text('Acoustic'), findsOneWidget);
    expect(find.text('Chill chat'), findsOneWidget);
    expect(find.textContaining('Requests open'), findsOneWidget);
    expect(find.byKey(const Key('following_chip')), findsOneWidget);

    await t.tap(find.byKey(const Key('topic_music')));
    await settle(t);
    expect(a.requests.last.queryParameters['topic'], 'music');
    expect(find.text('Chill chat'), findsNothing);

    await t.tap(find.byKey(const Key('filter_following')));
    await settle(t);
    expect(a.requests.last.queryParameters['following'], '1');
  });

  testWidgets('the host picks a topic and pins a message; viewers see the pin', (t) async {
    Object? started;
    Object? pinned;
    var pin = '';
    final a = await pumpWith(t, const BroadcastScreen(pollInterval: Duration(milliseconds: 100)), (o) async {
      if (o.method == 'POST' && o.path == '/live/rooms') {
        started = o.data;
        return Reply(201, {
          'room': {'id': 'r1', 'title': 'Hello', 'host_ref': 'h', 'host_name': 'Me', 'mine': true, 'topic': 'music'},
          'connect': {'url': 'wss://lk.example', 'token': 'T', 'room': 'r1', 'can_publish': true},
        });
      }
      if (o.path == '/live/rooms/r1/pin') {
        pinned = o.data;
        pin = (o.data as Map)['text'] as String;
        return Reply(204);
      }
      if (o.path == '/live/rooms/r1/events') return Reply(200, {'events': [], 'pinned': pin});
      if (o.method == 'DELETE') return Reply(204);
      return Reply(404);
    });
    await t.enterText(find.byKey(const Key('stream_title')), 'Hello');
    await t.tap(find.byKey(const Key('stream_topic')));
    await t.pumpAndSettle();
    await t.tap(find.text('Music').last);
    await t.pumpAndSettle();
    await t.tap(find.byKey(const Key('start_stream')));
    await settle(t);
    expect(started, {'title': 'Hello', 'topic': 'music', 'theme': 'plain'});

    await t.tap(find.byKey(const Key('pin_message')));
    await t.pumpAndSettle();
    await t.enterText(find.byKey(const Key('pin_text')), 'Requests open');
    await t.tap(find.byKey(const Key('save_pin')));
    await settle(t);
    expect(pinned, {'text': 'Requests open'});
    expect(find.text('Requests open'), findsOneWidget);
    expect(a.count('/live/rooms/r1/pin'), 1);

    // Tear down the stream so the poll timer stops.
    await t.tap(find.byKey(const Key('end_stream')));
    await settle(t);
  });

  testWidgets('viewers see the pinned banner from the events poll', (t) async {
    await pumpWith(t, const LiveRoomScreen(roomId: 'r7', pollInterval: Duration(milliseconds: 100)), (o) async {
      if (o.path == '/live/rooms/r7/join') return Reply(200, {'url': 'wss://lk.example', 'token': 'T', 'can_publish': false});
      if (o.path == '/live/rooms/r7/events') return Reply(200, {'events': [], 'pinned': 'Be kind in chat'});
      return Reply(404);
    });
    await t.pump(const Duration(milliseconds: 300));
    expect(find.byKey(const Key('pinned_banner')), findsOneWidget);
    expect(find.text('Be kind in chat'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
  });
}
