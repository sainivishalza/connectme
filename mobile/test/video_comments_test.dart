import 'package:connectme/core/providers.dart';
import 'package:connectme/features/videos/screens/video_feed_screen.dart';
import 'package:connectme/features/videos/widgets/video_view.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

Map<String, dynamic> clip(String id, {int comments = 0, bool mine = false}) => {
      'id': id, 'kind': 'short', 'caption': 'A clip', 'seconds': 8, 'likes': 0, 'mine': mine, 'comments': comments,
      'created_at': DateTime.now().subtract(const Duration(minutes: 3)).toUtc().toIso8601String(),
      'author': {'ref': 'a', 'name': 'Dana'},
    };

void main() {
  testWidgets('comments open from a clip, can be added, and the count follows', (t) async {
    t.view.physicalSize = const Size(800, 1800);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.reset);
    final comments = <Map<String, dynamic>>[
      {'id': 'c1', 'body': 'Lovely', 'name': 'Someone', 'mine': false},
    ];
    final client = fakeClient((o) async {
      if (o.path == '/videos/feed') return Reply(200, {'videos': [clip('v1', comments: comments.length)]});
      if (o.path == '/videos/v1/comments' && o.method == 'GET') return Reply(200, {'comments': comments});
      if (o.path == '/videos/v1/comments' && o.method == 'POST') {
        comments.add({'id': 'c2', 'body': (o.data as Map)['body'], 'name': 'Me', 'mine': true});
        return Reply(201, comments.last);
      }
      return Reply(404);
    });
    await t.pumpWidget(ProviderScope(
      overrides: [
        apiClientProvider.overrideWithValue(client),
        videoViewBuilderProvider.overrideWithValue((id, {autoplay = true}) => const SizedBox()),
      ],
      child: const MaterialApp(home: VideoFeedScreen()),
    ));
    await t.pumpAndSettle();
    await t.tap(find.byKey(const Key('comments_v1')));
    await t.pumpAndSettle();
    expect(find.text('Lovely'), findsOneWidget);
    await t.enterText(find.byKey(const Key('comment_text')), 'Great clip!');
    await t.pump();
    await t.tap(find.byKey(const Key('send_comment')));
    await t.pumpAndSettle();
    expect(find.text('Great clip!'), findsOneWidget);
    expect((client.dio.httpClientAdapter as FakeAdapter).requests.where((r) => r.method == 'POST').length, 1);
  });

  testWidgets('the Following filter reloads the feed from people you follow', (t) async {
    final client = fakeClient((o) async {
      if (o.path == '/videos/feed') {
        return Reply(200, {'videos': o.queryParameters['following'] == '1' ? [] : [clip('v1')]});
      }
      return Reply(404);
    });
    await t.pumpWidget(ProviderScope(
      overrides: [
        apiClientProvider.overrideWithValue(client),
        videoViewBuilderProvider.overrideWithValue((id, {autoplay = true}) => const SizedBox()),
      ],
      child: const MaterialApp(home: VideoFeedScreen()),
    ));
    await t.pumpAndSettle();
    await t.tap(find.byKey(const Key('videos_following')));
    await t.pumpAndSettle();
    expect((client.dio.httpClientAdapter as FakeAdapter).requests.last.queryParameters['following'], '1');
    expect(find.text('Nothing from people you follow'), findsOneWidget);
  });
}
