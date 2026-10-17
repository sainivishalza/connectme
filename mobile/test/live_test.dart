import 'package:connectme/core/network/api_client.dart';
import 'package:connectme/core/providers.dart';
import 'package:connectme/features/live/media_session.dart';
import 'package:connectme/features/live/screens/broadcast_screen.dart';
import 'package:connectme/features/live/screens/live_list_screen.dart';
import 'package:connectme/features/live/screens/live_room_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

class FakeMedia implements LiveMedia {
  String? url, token;
  bool? publish;
  bool disconnected = false;

  @override
  Future<void> connect(String url, String token, {required bool publish}) async {
    this.url = url;
    this.token = token;
    this.publish = publish;
  }

  @override
  Widget view() => const ColoredBox(color: Colors.black, child: Text('video'));

  @override
  Future<void> disconnect() async => disconnected = true;
}

Future<(FakeAdapter, FakeMedia)> pump(WidgetTester t, Widget home, Handler h) async {
  final client = fakeClient(h);
  final adapter = client.dio.httpClientAdapter as FakeAdapter;
  final media = FakeMedia();
  await t.pumpWidget(ProviderScope(
    overrides: [
      apiClientProvider.overrideWithValue(client),
      liveMediaFactoryProvider.overrideWithValue(() => media),
    ],
    child: MaterialApp(home: home),
  ));
  await t.pumpAndSettle();
  return (adapter, media);
}

void main() {
  retryTests();
  testWidgets('a 404 from the server shows an error with retry, not an endless spinner', (t) async {
    final client = fakeClient((o) async => Reply(404, {'error': 'not found'}));
    await t.pumpWidget(ProviderScope(
      retry: appRetry,
      overrides: [apiClientProvider.overrideWithValue(client)],
      child: const MaterialApp(home: LiveListScreen()),
    ));
    // No pumpAndSettle: it fast-forwards retry timers and would hide the bug.
    await t.pump();
    await t.pump(const Duration(milliseconds: 100));
    expect(find.text('Try again'), findsOneWidget, reason: 'a 404 must surface at once, not after retries');
  });
  testWidgets('host sees chat while live and can mute a viewer from a line', (t) async {
    final (adapter, _) = await pump(t, const BroadcastScreen(pollInterval: Duration(milliseconds: 100)), (o) async {
      if (o.method == 'POST' && o.path == '/live/rooms') {
        return Reply(201, {
          'room': {'id': 'r9', 'title': 'Hello', 'host_ref': 'h', 'host_name': 'Me', 'mine': true},
          'connect': {'url': 'wss://lk.example', 'token': 'HOSTTOK', 'room': 'r9', 'can_publish': true},
        });
      }
      if (o.path == '/live/rooms/r9/events') {
        final after = o.queryParameters['after'];
        return Reply(200, {
          'events': after == 0
              ? [
                  {'id': 4, 'ref': 'viewer-ref', 'kind': 'chat', 'name': 'Rude', 'text': 'something rude'}
                ]
              : []
        });
      }
      if (o.path == '/live/rooms/r9/moderate') return Reply(204);
      if (o.method == 'DELETE') return Reply(204);
      return Reply(404, {'error': 'nope'});
    });
    await t.enterText(find.byKey(const Key('stream_title')), 'Hello world');
    await t.tap(find.byKey(const Key('start_stream')));
    await t.pump(const Duration(milliseconds: 50));
    await t.pump(const Duration(milliseconds: 200));
    expect(find.textContaining('something rude', findRichText: true), findsOneWidget);

    await t.tap(find.byKey(const ValueKey('chat_line_4')));
    await t.pumpAndSettle(const Duration(milliseconds: 50));
    await t.tap(find.byKey(const Key('mute_viewer')));
    await t.pump(const Duration(milliseconds: 100));
    final req = adapter.requests.firstWhere((r) => r.path == '/live/rooms/r9/moderate').data as Map;
    expect(req['ref'], 'viewer-ref');
    expect(req['action'], 'mute');
    await t.pump(const Duration(seconds: 5)); // let the confirmation snackbar expire
    await t.pumpWidget(const SizedBox());
    await t.pump(const Duration(milliseconds: 50)); // leaving ends the room: let that request finish
  });

  testWidgets('chat lines and gift announcements arrive by polling; sending chat posts text', (t) async {
    var served = false;
    final (adapter, _) = await pump(t, const LiveRoomScreen(roomId: 'r1', pollInterval: Duration(milliseconds: 200)), (o) async {
      if (o.path == '/live/rooms/r1/join') {
        return Reply(200, {'url': 'wss://lk.example', 'token': 'T', 'room': 'r1', 'can_publish': false});
      }
      if (o.path == '/live/rooms/r1/events') {
        final after = o.queryParameters['after'];
        if (after == 0 && !served) {
          served = true;
          return Reply(200, {
            'events': [
              {'id': 1, 'kind': 'chat', 'name': 'Ana', 'text': 'hello there'},
            ]
          });
        }
        if (after == 1) {
          return Reply(200, {
            'events': [
              {'id': 2, 'kind': 'gift', 'name': 'Ben', 'text': 'sent a Rose'},
            ]
          });
        }
        return Reply(200, {'events': []});
      }
      if (o.path == '/live/rooms/r1/chat') return Reply(204);
      return Reply(404, {'error': 'nope'});
    });
    await t.pump(const Duration(milliseconds: 100));
    expect(find.textContaining('hello there', findRichText: true), findsOneWidget);
    await t.pump(const Duration(milliseconds: 250));
    expect(find.text('Ben sent a Rose'), findsOneWidget);

    await t.enterText(find.byKey(const Key('live_chat_input')), 'nice!');
    await t.tap(find.byKey(const Key('live_chat_send')));
    await t.pump(const Duration(milliseconds: 50));
    final sent = adapter.requests.firstWhere((r) => r.path == '/live/rooms/r1/chat').data as Map;
    expect(sent['text'], 'nice!');
    await t.pumpWidget(const SizedBox());
    await t.pump(const Duration(seconds: 4)); // let the toast timer fire after disposal
  });

  testWidgets('a stream that ends while watching is announced and polling stops', (t) async {
    var calls = 0;
    await pump(t, const LiveRoomScreen(roomId: 'r1', pollInterval: Duration(milliseconds: 100)), (o) async {
      if (o.path == '/live/rooms/r1/join') {
        return Reply(200, {'url': 'wss://lk.example', 'token': 'T', 'room': 'r1', 'can_publish': false});
      }
      if (o.path == '/live/rooms/r1/events') {
        calls++;
        return Reply(410, {'error': 'this stream has ended'});
      }
      return Reply(404, {'error': 'nope'});
    });
    await t.pump(const Duration(milliseconds: 150));
    expect(find.byKey(const Key('stream_ended')), findsOneWidget);
    final before = calls;
    await t.pump(const Duration(milliseconds: 500));
    expect(calls, before, reason: 'no further polling after the stream ended');
    await t.pumpWidget(const SizedBox());
  });

  testWidgets('list shows live rooms and an empty state', (t) async {
    await pump(t, const LiveListScreen(), (o) async => Reply(200, {
          'rooms': [
            {'id': 'r1', 'title': 'Friday chat', 'host_ref': 'h', 'host_name': 'Ana', 'mine': false},
          ]
        }));
    expect(find.text('Friday chat'), findsOneWidget);
    expect(find.text('Ana'), findsOneWidget);

    await pump(t, const LiveListScreen(), (o) async => Reply(200, {'rooms': []}));
    expect(find.text('Nobody is live'), findsOneWidget);
  });

  testWidgets('viewer joins subscribe-only with the server token and can gift', (t) async {
    final (adapter, media) = await pump(t, const LiveRoomScreen(roomId: 'r1'), (o) async {
      if (o.path == '/live/rooms/r1/join') {
        return Reply(200, {'url': 'wss://lk.example', 'token': 'TOK', 'room': 'r1', 'can_publish': false});
      }
      if (o.path == '/payments/gifts/catalog') {
        return Reply(200, {
          'gifts': [
            {'id': 'rose', 'name': 'Rose', 'price_coins': 10}
          ]
        });
      }
      if (o.path == '/live/rooms/r1/gift') return Reply(200, {'coins': 10, 'remaining': 90, 'replayed': false});
      if (o.path == '/live/rooms/r1/events') return Reply(200, {'events': []});
      return Reply(404, {'error': 'nope'});
    });
    expect(media.url, 'wss://lk.example');
    expect(media.token, 'TOK');
    expect(media.publish, false);
    expect(find.text('video'), findsOneWidget);

    await t.tap(find.byKey(const Key('send_live_gift')));
    await t.pumpAndSettle();
    await t.tap(find.byKey(const Key('live_gift_rose')));
    await t.pumpAndSettle();

    final body = adapter.requests.firstWhere((r) => r.path == '/live/rooms/r1/gift').data as Map;
    expect(body['gift_id'], 'rose');
    expect((body['request_id'] as String).length, greaterThanOrEqualTo(8));
    await t.pumpWidget(const SizedBox()); // dispose the screen and its poll timer
  });

  testWidgets('a room that has ended shows the error with retry', (t) async {
    await pump(t, const LiveRoomScreen(roomId: 'r1'),
        (o) async => Reply(410, {'error': 'this stream has ended'}));
    expect(find.text('this stream has ended'), findsOneWidget);
  });

  testWidgets('broadcast starts, publishes, and ends the room', (t) async {
    final (adapter, media) = await pump(t, const BroadcastScreen(), (o) async {
      if (o.method == 'POST' && o.path == '/live/rooms') {
        return Reply(201, {
          'room': {'id': 'r9', 'title': 'Hello', 'host_ref': 'h', 'host_name': 'Me', 'mine': true},
          'connect': {'url': 'wss://lk.example', 'token': 'HOSTTOK', 'room': 'r9', 'can_publish': true},
        });
      }
      if (o.method == 'DELETE' && o.path == '/live/rooms/r9') return Reply(204);
      return Reply(404, {'error': 'nope'});
    });
    await t.enterText(find.byKey(const Key('stream_title')), 'Hello world');
    await t.tap(find.byKey(const Key('start_stream')));
    await t.pumpAndSettle();
    expect(media.publish, true);
    expect(media.token, 'HOSTTOK');
    expect(find.byKey(const Key('end_stream')), findsOneWidget);

    await t.tap(find.byKey(const Key('end_stream')));
    await t.pumpAndSettle();
    expect(adapter.requests.any((r) => r.method == 'DELETE' && r.path == '/live/rooms/r9'), true);
    expect(media.disconnected, true);
  });

  testWidgets('a short title is rejected before any request', (t) async {
    final (adapter, _) = await pump(t, const BroadcastScreen(), (o) async => Reply(500));
    await t.enterText(find.byKey(const Key('stream_title')), 'ab');
    await t.tap(find.byKey(const Key('start_stream')));
    await t.pumpAndSettle();
    expect(adapter.requests, isEmpty);
  });

  testWidgets('connect failure after the room was created ends the room', (t) async {
    final media = _FailingMedia();
    final client = fakeClient((o) async {
      if (o.method == 'POST') {
        return Reply(201, {
          'room': {'id': 'r5', 'title': 'x', 'host_ref': 'h', 'host_name': 'Me', 'mine': true},
          'connect': {'url': 'wss://x', 'token': 'T', 'room': 'r5', 'can_publish': true},
        });
      }
      return Reply(204);
    });
    final adapter = client.dio.httpClientAdapter as FakeAdapter;
    await t.pumpWidget(ProviderScope(
      overrides: [
        apiClientProvider.overrideWithValue(client),
        liveMediaFactoryProvider.overrideWithValue(() => media),
      ],
      child: const MaterialApp(home: BroadcastScreen()),
    ));
    await t.enterText(find.byKey(const Key('stream_title')), 'Camera denied');
    await t.tap(find.byKey(const Key('start_stream')));
    await t.pumpAndSettle();
    expect(adapter.requests.any((r) => r.method == 'DELETE' && r.path == '/live/rooms/r5'), true,
        reason: 'no orphaned live room when the camera cannot start');
    expect(find.byKey(const Key('start_stream')), findsOneWidget);
  });
}

class _FailingMedia extends FakeMedia {
  @override
  Future<void> connect(String url, String token, {required bool publish}) => throw StateError('camera denied');
}

void retryTests() {
  test('appRetry never retries client errors and only briefly retries network/server errors', () {
    expect(appRetry(0, ApiException(404, 'nope')), isNull);
    expect(appRetry(0, ApiException(403, 'no')), isNull);
    expect(appRetry(0, ApiException(422, 'bad')), isNull);
    expect(appRetry(0, ApiException(0, 'offline')), const Duration(seconds: 1));
    expect(appRetry(1, ApiException(503, 'down')), const Duration(seconds: 2));
    expect(appRetry(2, ApiException(503, 'down')), isNull, reason: 'gives up after two retries');
    expect(appRetry(0, StateError('bug')), isNull, reason: 'programming errors are not retried');
  });
}
