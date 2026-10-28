import 'package:connectme/core/providers.dart';
import 'package:connectme/features/live/media_session.dart';
import 'package:connectme/features/live/screens/broadcast_screen.dart';
import 'package:connectme/features/live/screens/live_room_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

class _Media implements LiveMedia {
  bool? published;
  @override
  Future<void> connect(String url, String token, {required bool publish}) async => published = publish;
  @override
  Widget view() => const ColoredBox(color: Colors.black);
  @override
  Future<void> disconnect() async {}
}

void main() {
  testWidgets('the host invites a viewer from a chat line', (t) async {
    final client = fakeClient((o) async {
      if (o.method == 'POST' && o.path == '/live/rooms') {
        return Reply(201, {
          'room': {'id': 'r9', 'title': 'Hello', 'host_ref': 'h', 'host_name': 'Me', 'mine': true},
          'connect': {'url': 'wss://lk.example', 'token': 'T', 'room': 'r9', 'can_publish': true},
        });
      }
      if (o.path == '/live/rooms/r9/events') {
        return Reply(200, {
          'events': o.queryParameters['after'] == 0
              ? [
                  {'id': 4, 'kind': 'chat', 'ref': 'viewer-ref', 'name': 'Vee', 'text': 'love this'}
                ]
              : [],
          'pinned': '',
          'cohosts': []
        });
      }
      if (o.path == '/live/rooms/r9/stage/invite') return Reply(204);
      return Reply(204);
    });
    await t.pumpWidget(ProviderScope(
      overrides: [apiClientProvider.overrideWithValue(client), liveMediaFactoryProvider.overrideWithValue(() => _Media())],
      child: const MaterialApp(home: BroadcastScreen(pollInterval: Duration(milliseconds: 100))),
    ));
    await t.pumpAndSettle();
    await t.enterText(find.byKey(const Key('stream_title')), 'Hello world');
    await t.tap(find.byKey(const Key('start_stream')));
    await t.pump(const Duration(milliseconds: 50));
    await t.pump(const Duration(milliseconds: 250));
    await t.tap(find.byKey(const ValueKey('chat_line_4')));
    await t.pumpAndSettle(const Duration(milliseconds: 50));
    await t.tap(find.byKey(const Key('make_cohost')));
    await t.pump(const Duration(milliseconds: 100));
    final req = (client.dio.httpClientAdapter as FakeAdapter).requests.firstWhere((r) => r.path == '/live/rooms/r9/stage/invite').data as Map;
    expect(req, {'ref': 'viewer-ref'});
    await t.pump(const Duration(seconds: 5));
    await t.pumpWidget(const SizedBox());
    await t.pump(const Duration(milliseconds: 50));
  });

  testWidgets('a co-host joins publishing, and viewers see who is on with the host', (t) async {
    final media = _Media();
    final client = fakeClient((o) async {
      if (o.path == '/live/rooms/r1/join') return Reply(200, {'url': 'wss://lk.example', 'token': 'T', 'room': 'r1', 'can_publish': true, 'cohost': true});
      if (o.path == '/live/rooms/r1/events') return Reply(200, {'events': [], 'pinned': '', 'cohosts': ['Mina']});
      return Reply(404);
    });
    await t.pumpWidget(ProviderScope(
      overrides: [apiClientProvider.overrideWithValue(client), liveMediaFactoryProvider.overrideWithValue(() => media)],
      child: const MaterialApp(home: LiveRoomScreen(roomId: 'r1', pollInterval: Duration(milliseconds: 100))),
    ));
    await t.pump(const Duration(milliseconds: 400));
    expect(media.published, isTrue);
    expect(find.text('With Mina'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
  });
}
