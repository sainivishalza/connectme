import 'dart:typed_data';

import 'package:connectme/core/providers.dart';
import 'package:connectme/features/videos/screens/video_feed_screen.dart';
import 'package:connectme/features/videos/videos_repository.dart';
import 'package:connectme/features/videos/widgets/reel_section.dart';
import 'package:connectme/features/videos/widgets/video_view.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_riverpod/misc.dart' show Override;
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

Map<String, dynamic> clipJson(String id, {String caption = 'Sunset run', bool mine = false, int likes = 0, bool liked = false, String kind = 'short'}) => {
      'id': id,
      'kind': kind,
      'caption': caption,
      'seconds': 12,
      'created_at': DateTime.now().subtract(const Duration(minutes: 3)).toUtc().toIso8601String(),
      'likes': likes,
      'liked': liked,
      'mine': mine,
      if (kind == 'short') 'author': {'ref': 'a-$id', 'name': 'Dana'},
    };

final List<Override> _common = [
  // No codec in tests: the "player" is just a label with the clip id.
  videoViewBuilderProvider.overrideWithValue((id, {autoplay = true}) => Center(child: Text('playing $id', key: Key('playing_$id')))),
];

Future<FakeAdapter> pumpWith(WidgetTester t, Handler h, Widget home, {List<Override> extra = const []}) async {
  final client = fakeClient(h);
  await t.pumpWidget(ProviderScope(
    overrides: [apiClientProvider.overrideWithValue(client), ..._common, ...extra],
    child: MaterialApp(home: home),
  ));
  await t.pumpAndSettle();
  return client.dio.httpClientAdapter as FakeAdapter;
}

void main() {
  testWidgets('the feed plays the visible clip, likes it, and lets the owner delete', (t) async {
    var liked = false;
    await pumpWith(t, (o) async {
      if (o.path == '/videos/feed') {
        return Reply(200, {
          'videos': [clipJson('v1', likes: 2), clipJson('v2', caption: 'Mine', mine: true)]
        });
      }
      if (o.path == '/videos/v1/like') {
        liked = !liked;
        return Reply(200, {'liked': liked, 'likes': liked ? 3 : 2});
      }
      if (o.path == '/videos/v2' && o.method == 'DELETE') return Reply(204);
      return Reply(404);
    }, const VideoFeedScreen());
    expect(find.text('Sunset run'), findsOneWidget);
    expect(find.text('Dana'), findsOneWidget);
    expect(find.byKey(const Key('playing_v1')), findsOneWidget);
    expect(find.byKey(const Key('playing_v2')), findsNothing, reason: 'only the visible page plays');

    await t.tap(find.byKey(const Key('like_v1')));
    await t.pumpAndSettle();
    expect(find.text('3'), findsOneWidget);
    expect(find.byIcon(Icons.favorite), findsOneWidget);

    // Swipe up to my own clip: it offers delete, not report.
    await t.drag(find.byType(PageView), const Offset(0, -600));
    await t.pumpAndSettle();
    expect(find.byKey(const Key('playing_v2')), findsOneWidget);
    expect(find.byKey(const Key('delete_v2')), findsOneWidget);
    expect(find.byKey(const Key('report_v2')), findsNothing);
    await t.tap(find.byKey(const Key('delete_v2')));
    await t.pumpAndSettle();
    expect(find.text('Mine'), findsNothing);
  });

  testWidgets('reporting asks for a reason and sends it', (t) async {
    String? reason;
    await pumpWith(t, (o) async {
      if (o.path == '/videos/feed') {
        return Reply(200, {
          'videos': [clipJson('v1')]
        });
      }
      if (o.path == '/videos/v1/report') {
        reason = (o.data as Map)['reason'] as String;
        return Reply(204);
      }
      return Reply(404);
    }, const VideoFeedScreen());
    await t.tap(find.byKey(const Key('report_v1')));
    await t.pumpAndSettle();
    await t.tap(find.byKey(const Key('report_scam')));
    await t.pumpAndSettle();
    expect(reason, 'scam');
    expect(find.text('Thanks. Our team will take a look.'), findsOneWidget);
  });

  testWidgets('an empty feed invites the first post, and posting uploads the picked clip', (t) async {
    var posted = false;
    Uri? uploadUri;
    await pumpWith(
      t,
      (o) async {
        if (o.path == '/videos/feed') {
          return Reply(200, {'videos': posted ? [clipJson('v9', caption: 'Hello')] : []});
        }
        if (o.method == 'POST' && o.path.startsWith('/videos/')) {
          uploadUri = o.uri;
          posted = true;
          return Reply(201, {'video': clipJson('v9', caption: 'Hello', mine: true)});
        }
        return Reply(404);
      },
      const VideoFeedScreen(),
      extra: [videoPickerProvider.overrideWithValue(({required bool camera}) async => PickedVideo(Uint8List.fromList([1, 2, 3])))],
    );
    expect(find.text('No videos yet'), findsOneWidget);
    await t.tap(find.byKey(const Key('post_video')));
    await t.pumpAndSettle();
    await t.tap(find.byKey(const Key('pick_gallery')));
    await t.pumpAndSettle();
    await t.enterText(find.byKey(const Key('video_caption')), 'Hello');
    await t.tap(find.byKey(const Key('video_post')));
    // the dialog closes, then the upload and the refreshed feed arrive
    for (var i = 0; i < 4; i++) {
      await t.pump(const Duration(milliseconds: 300));
    }
    expect(uploadUri?.queryParameters, {'kind': 'short', 'caption': 'Hello'});
    expect(find.text('Hello'), findsOneWidget);
  });

  testWidgets('the profile reel section uploads, shows, plays and removes the reel', (t) async {
    var has = false;
    String? kind;
    await pumpWith(
      t,
      (o) async {
        if (o.path == '/videos/reel/me-ref') return has ? Reply(200, {'video': clipJson('r1', kind: 'reel', mine: true, caption: '')}) : Reply(404, {'error': 'not found'});
        if (o.method == 'POST' && o.path.startsWith('/videos/')) {
          kind = o.uri.queryParameters['kind'];
          has = true;
          return Reply(201, {'video': clipJson('r1', kind: 'reel', mine: true, caption: '')});
        }
        if (o.path == '/videos/r1' && o.method == 'DELETE') {
          has = false;
          return Reply(204);
        }
        return Reply(404);
      },
      const Scaffold(body: SingleChildScrollView(child: ReelSection(myRef: 'me-ref'))),
      extra: [videoPickerProvider.overrideWithValue(({required bool camera}) async => PickedVideo(Uint8List.fromList([9])))],
    );
    expect(find.text('Add a video'), findsOneWidget);
    expect(find.byKey(const Key('watch_reel')), findsNothing);

    await t.tap(find.byKey(const Key('add_reel')));
    await t.pumpAndSettle();
    expect(kind, 'reel');
    expect(find.text('Replace'), findsOneWidget);

    await t.tap(find.byKey(const Key('watch_reel')));
    await t.pumpAndSettle();
    expect(find.byKey(const Key('playing_r1')), findsOneWidget);
    await t.tap(find.byKey(const Key('close_clip')));
    await t.pumpAndSettle();

    await t.tap(find.byKey(const Key('remove_reel')));
    await t.pumpAndSettle();
    expect(find.text('Add a video'), findsOneWidget);
  });

  testWidgets('a held reel tells its owner it is under review', (t) async {
    await pumpWith(
      t,
      (o) async {
        if (o.path == '/videos/reel/me-ref') {
          return Reply(200, {'video': {...clipJson('r1', kind: 'reel', mine: true, caption: ''), 'status': 'held'}});
        }
        return Reply(404);
      },
      const Scaffold(body: SingleChildScrollView(child: ReelSection(myRef: 'me-ref'))),
    );
    expect(find.byKey(const Key('reel_held')), findsOneWidget);
    expect(find.byKey(const Key('watch_reel')), findsOneWidget);
  });
}
