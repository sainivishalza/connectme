import 'package:connectme/core/providers.dart';
import 'package:connectme/features/live/media_session.dart';
import 'package:connectme/features/live/screens/broadcast_screen.dart';
import 'package:connectme/features/live/screens/live_room_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

class _StageMedia implements LiveMedia, StageMedia {
  final calls = <String>[];
  @override
  Future<void> connect(String url, String token, {required bool publish}) async => calls.add('connect publish=$publish');
  @override
  Future<void> setPublishing(bool on) async => calls.add('publishing=$on');
  @override
  Widget view() => const ColoredBox(color: Colors.black);
  @override
  Future<void> disconnect() async {}
}

Future<void> ticks(WidgetTester t) async {
  await t.pump(const Duration(milliseconds: 400));
  await t.pump(const Duration(milliseconds: 400));
}

void main() {
  testWidgets('a viewer asks to join, then the camera starts when the host lets them up, and stops when they leave', (t) async {
    final media = _StageMedia();
    var me = 'none';
    final seen = <String>[];
    final client = fakeClient((o) async {
      if (o.path == '/live/rooms/r1/join') return Reply(200, {'url': 'wss://lk.example', 'token': 'T', 'room': 'r1', 'can_publish': false});
      if (o.path == '/live/rooms/r1/events') {
        return Reply(200, {'events': [], 'pinned': '', 'cohosts': [], 'stage': {'me': me, 'requests': [], 'guests': me == 'on_stage' ? 1 : 0, 'max': 7}});
      }
      if (o.path == '/live/rooms/r1/stage/request') {
        seen.add('request');
        me = 'requested';
        return Reply(204);
      }
      if (o.path == '/live/rooms/r1/stage/leave') {
        seen.add('leave');
        me = 'none';
        return Reply(204);
      }
      return Reply(404);
    });
    await t.pumpWidget(ProviderScope(
      overrides: [apiClientProvider.overrideWithValue(client), liveMediaFactoryProvider.overrideWithValue(() => media)],
      child: const MaterialApp(home: LiveRoomScreen(roomId: 'r1', pollInterval: Duration(milliseconds: 100))),
    ));
    await ticks(t);
    expect(find.byKey(const Key('ask_to_join')), findsOneWidget);

    await t.tap(find.byKey(const Key('ask_to_join')));
    await ticks(t);
    expect(seen, ['request']);
    expect(find.byKey(const Key('cancel_request')), findsOneWidget);

    // The host lets them up: the next poll says on_stage and the camera turns on.
    me = 'on_stage';
    await ticks(t);
    expect(media.calls, contains('publishing=true'));
    expect(find.byKey(const Key('leave_stage')), findsOneWidget);

    await t.tap(find.byKey(const Key('leave_stage')));
    await ticks(t);
    expect(seen, ['request', 'leave']);
    expect(media.calls.last, 'publishing=false');
    expect(find.byKey(const Key('ask_to_join')), findsOneWidget);
    await t.pumpWidget(const SizedBox());
  });

  testWidgets('a viewer who is invited can join or say not now', (t) async {
    final media = _StageMedia();
    var me = 'invited';
    String? answer;
    final client = fakeClient((o) async {
      if (o.path == '/live/rooms/r1/join') return Reply(200, {'url': 'wss://lk.example', 'token': 'T', 'room': 'r1', 'can_publish': false});
      if (o.path == '/live/rooms/r1/events') {
        return Reply(200, {'events': [], 'pinned': '', 'cohosts': [], 'stage': {'me': me, 'requests': [], 'guests': 0, 'max': 7}});
      }
      if (o.path == '/live/rooms/r1/stage/respond') {
        answer = '${(o.data as Map)['accept']}';
        me = (o.data as Map)['accept'] == true ? 'on_stage' : 'none';
        return Reply(204);
      }
      return Reply(404);
    });
    await t.pumpWidget(ProviderScope(
      overrides: [apiClientProvider.overrideWithValue(client), liveMediaFactoryProvider.overrideWithValue(() => media)],
      child: const MaterialApp(home: LiveRoomScreen(roomId: 'r1', pollInterval: Duration(milliseconds: 100))),
    ));
    await ticks(t);
    expect(find.byKey(const Key('stage_invite')), findsOneWidget);
    await t.tap(find.byKey(const Key('accept_invite')));
    await ticks(t);
    expect(answer, 'true');
    expect(media.calls, contains('publishing=true'));
    expect(find.byKey(const Key('stage_invite')), findsNothing);
    await t.pumpWidget(const SizedBox());
  });

  testWidgets('the host sees who is asking to join and can let several people up', (t) async {
    // A narrow phone: both buttons must stay visible next to the name.
    t.view.physicalSize = const Size(360, 740);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.reset);
    var requests = [
      {'ref': 'a-ref', 'name': 'Asha'},
      {'ref': 'b-ref', 'name': 'Ben'},
    ];
    final decided = <String>[];
    final client = fakeClient((o) async {
      if (o.method == 'POST' && o.path == '/live/rooms') {
        return Reply(201, {
          'room': {'id': 'r9', 'title': 'Hello', 'host_ref': 'h', 'host_name': 'Me', 'mine': true},
          'connect': {'url': 'wss://lk.example', 'token': 'T', 'room': 'r9', 'can_publish': true},
        });
      }
      if (o.path == '/live/rooms/r9/events') {
        return Reply(200, {'events': [], 'pinned': '', 'cohosts': [], 'stage': {'me': 'on_stage', 'requests': requests, 'guests': 0, 'max': 7}});
      }
      if (o.path == '/live/rooms/r9/stage/decide') {
        final d = o.data as Map;
        decided.add('${d['ref']}:${d['accept']}');
        requests = [for (final r in requests) if (r['ref'] != d['ref']) r];
        return Reply(204);
      }
      return Reply(204);
    });
    await t.pumpWidget(ProviderScope(
      overrides: [apiClientProvider.overrideWithValue(client), liveMediaFactoryProvider.overrideWithValue(() => _StageMedia())],
      child: const MaterialApp(home: BroadcastScreen(pollInterval: Duration(milliseconds: 100))),
    ));
    await t.pumpAndSettle();
    await t.enterText(find.byKey(const Key('stream_title')), 'Hello world');
    await t.tap(find.byKey(const Key('start_stream')));
    await ticks(t);
    expect(find.text('Asking to join (2)'), findsOneWidget);

    await t.tap(find.byKey(const Key('stage_requests')));
    await t.pumpAndSettle(const Duration(milliseconds: 50));
    expect(find.text('Asha'), findsOneWidget);
    expect(find.text('Ben'), findsOneWidget);
    for (final k in ['accept_req_a-ref', 'decline_req_a-ref', 'accept_req_b-ref', 'decline_req_b-ref']) {
      final r = t.getRect(find.byKey(Key(k)));
      expect(r.right, lessThanOrEqualTo(360), reason: '$k is cut off');
    }
    await t.tap(find.byKey(const Key('accept_req_a-ref')));
    await t.pump(const Duration(milliseconds: 200));
    await t.tap(find.byKey(const Key('decline_req_b-ref')));
    await t.pump(const Duration(milliseconds: 200));
    expect(decided, ['a-ref:true', 'b-ref:false']);
    await t.pump(const Duration(seconds: 5));
    await t.pumpWidget(const SizedBox());
    await t.pump(const Duration(milliseconds: 50));
  });

  test('the camera grid gives one, two, then up to eight people room', () {
    expect([for (var n = 1; n <= 8; n++) CameraGrid.columnsFor(n)], [1, 2, 2, 2, 3, 3, 3, 3]);
  });
}
