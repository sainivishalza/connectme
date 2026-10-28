import 'package:connectme/core/providers.dart';
import 'package:connectme/features/live/media_session.dart';
import 'package:connectme/features/live/screens/broadcast_screen.dart';
import 'package:connectme/features/live/screens/live_room_screen.dart';
import 'package:connectme/features/live/live_repository.dart';
import 'package:connectme/features/live/widgets/room_theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

class _Media implements LiveMedia {
  @override
  Future<void> connect(String url, String token, {required bool publish}) async {}
  @override
  Widget view() => const ColoredBox(color: Colors.black);
  @override
  Future<void> disconnect() async {}
}

void main() {
  test('unknown themes fall back to plain', () {
    expect(RoomTheme.of('velvet').id, 'velvet');
    expect(RoomTheme.of('nonsense').id, 'plain');
    expect(RoomTheme.all.map((t) => t.id), ['plain', 'sunset', 'neon', 'forest', 'ocean', 'velvet']);
  });

  testWidgets('the host picks a theme that is sent when starting, and sees the tint', (t) async {
    t.view.physicalSize = const Size(800, 2000);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.reset);
    Object? sent;
    final client = fakeClient((o) async {
      if (o.method == 'POST' && o.path == '/live/rooms') {
        sent = o.data;
        return Reply(201, {
          'room': {'id': 'r1', 'title': 'Hello', 'host_ref': 'h', 'host_name': 'Me', 'mine': true, 'theme': 'ocean'},
          'connect': {'url': 'wss://lk', 'token': 'T', 'room': 'r1', 'can_publish': true},
        });
      }
      if (o.path == '/live/rooms/r1/events') return Reply(200, {'events': [], 'pinned': '', 'cohosts': []});
      return Reply(204);
    });
    await t.pumpWidget(ProviderScope(
      overrides: [apiClientProvider.overrideWithValue(client), liveMediaFactoryProvider.overrideWithValue(() => _Media())],
      child: const MaterialApp(home: BroadcastScreen(pollInterval: Duration(milliseconds: 100))),
    ));
    await t.pumpAndSettle();
    await t.enterText(find.byKey(const Key('stream_title')), 'Hello world');
    await t.tap(find.byKey(const Key('theme_ocean')));
    await t.pump();
    await t.tap(find.byKey(const Key('start_stream')));
    await t.pump(const Duration(milliseconds: 300));
    expect((sent! as Map)['theme'], 'ocean');
    expect(find.byKey(const Key('room_tint_ocean')), findsOneWidget);
    await t.tap(find.byKey(const Key('end_stream')));
    await t.pumpAndSettle();
  });

  testWidgets('viewers see the room tint from the room they opened', (t) async {
    final client = fakeClient((o) async {
      if (o.path == '/live/rooms/r2/join') return Reply(200, {'url': 'wss://lk', 'token': 'T', 'can_publish': false});
      if (o.path == '/live/rooms/r2/events') return Reply(200, {'events': [], 'pinned': '', 'cohosts': []});
      return Reply(404);
    });
    await t.pumpWidget(ProviderScope(
      overrides: [apiClientProvider.overrideWithValue(client), liveMediaFactoryProvider.overrideWithValue(() => _Media())],
      child: MaterialApp(
        home: LiveRoomScreen(
          roomId: 'r2',
          pollInterval: const Duration(milliseconds: 100),
          room: LiveRoom(id: 'r2', title: 'Night', hostRef: 'h', hostName: 'Ana', mine: false, theme: 'velvet'),
        ),
      ),
    ));
    await t.pump(const Duration(milliseconds: 400));
    expect(find.byKey(const Key('room_tint_velvet')), findsOneWidget);
    await t.pumpWidget(const SizedBox());
  });
}
