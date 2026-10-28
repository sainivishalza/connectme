import 'package:connectme/core/providers.dart';
import 'package:connectme/features/live/media_session.dart';
import 'package:connectme/features/live/screens/broadcast_screen.dart';
import 'package:connectme/features/live/screens/live_room_screen.dart';
import 'package:connectme/features/live/widgets/live_poll_card.dart';
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
  test('a poll is read from the server answer', () {
    final p = LivePoll.fromJson({'id': 'p', 'question': 'Q?', 'options': ['a', 'b'], 'counts': [1, 3], 'my_choice': 1, 'closed': false})!;
    expect((p.total, p.myChoice), (4, 1));
    expect(p.options, ['a', 'b']);
    expect(LivePoll.fromJson(null), isNull);
  });

  testWidgets('a viewer votes once and then sees the bars', (t) async {
    var voted = false;
    final client = fakeClient((o) async {
      if (o.path == '/live/rooms/r1/join') return Reply(200, {'url': 'wss://lk', 'token': 'T', 'can_publish': false});
      if (o.path == '/live/rooms/r1/poll/vote') {
        voted = true;
        return Reply(200, {});
      }
      if (o.path == '/live/rooms/r1/events') {
        return Reply(200, {
          'events': [],
          'pinned': '',
          'cohosts': [],
          'poll': {'id': 'p1', 'question': 'Pizza or tacos?', 'options': ['Pizza', 'Tacos'], 'counts': voted ? [0, 1] : [0, 0], if (voted) 'my_choice': 1},
        });
      }
      return Reply(404);
    });
    await t.pumpWidget(ProviderScope(
      overrides: [apiClientProvider.overrideWithValue(client), liveMediaFactoryProvider.overrideWithValue(() => _Media())],
      child: const MaterialApp(home: LiveRoomScreen(roomId: 'r1', pollInterval: Duration(milliseconds: 100))),
    ));
    await t.pump(const Duration(milliseconds: 400));
    expect(find.text('Pizza or tacos?'), findsOneWidget);
    expect(find.byKey(const Key('poll_count_0')), findsNothing, reason: 'no bars before voting');
    await t.tap(find.byKey(const Key('poll_option_1')));
    await t.pump(const Duration(milliseconds: 400));
    expect(voted, isTrue);
    expect(find.byKey(const Key('poll_count_1')), findsOneWidget);
    expect(find.text('1'), findsWidgets);
    await t.pumpWidget(const SizedBox());
  });

  testWidgets('the host starts a poll with two answers and can end it', (t) async {
    t.view.physicalSize = const Size(800, 2000);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.reset);
    Object? started;
    var open = false;
    final calls = <String>[];
    final client = fakeClient((o) async {
      if (o.method == 'POST' && o.path == '/live/rooms') {
        return Reply(201, {
          'room': {'id': 'r9', 'title': 'Hello', 'host_ref': 'h', 'host_name': 'Me', 'mine': true},
          'connect': {'url': 'wss://lk', 'token': 'T', 'room': 'r9', 'can_publish': true},
        });
      }
      if (o.path == '/live/rooms/r9/poll' && o.method == 'POST') {
        started = o.data;
        open = true;
        return Reply(201, {});
      }
      if (o.path == '/live/rooms/r9/poll' && o.method == 'DELETE') {
        calls.add('close');
        open = false;
        return Reply(204);
      }
      if (o.path == '/live/rooms/r9/events') {
        return Reply(200, {
          'events': [],
          'pinned': '',
          'cohosts': [],
          'poll': open ? {'id': 'p1', 'question': 'Night or morning?', 'options': ['Night', 'Morning'], 'counts': [2, 1]} : null,
        });
      }
      return Reply(204);
    });
    await t.pumpWidget(ProviderScope(
      overrides: [apiClientProvider.overrideWithValue(client), liveMediaFactoryProvider.overrideWithValue(() => _Media())],
      child: const MaterialApp(home: BroadcastScreen(pollInterval: Duration(milliseconds: 100))),
    ));
    await t.pumpAndSettle();
    await t.enterText(find.byKey(const Key('stream_title')), 'Game night');
    await t.tap(find.byKey(const Key('start_stream')));
    await t.pump(const Duration(milliseconds: 300));
    await t.tap(find.byKey(const Key('new_poll')));
    await t.pumpAndSettle();
    await t.enterText(find.byKey(const Key('poll_question')), 'Night or morning?');
    await t.enterText(find.byKey(const Key('poll_answer_0')), 'Night');
    await t.enterText(find.byKey(const Key('poll_answer_1')), 'Morning');
    await t.tap(find.byKey(const Key('poll_start')));
    await t.pump(const Duration(milliseconds: 500));
    expect(started, {'question': 'Night or morning?', 'options': ['Night', 'Morning']});
    expect(find.byKey(const Key('live_poll')), findsOneWidget);
    expect(find.byKey(const Key('poll_count_0')), findsOneWidget, reason: 'the host sees totals at once');
    await t.tap(find.byKey(const Key('poll_close')));
    await t.pump(const Duration(milliseconds: 400));
    expect(calls, ['close']);
    await t.tap(find.byKey(const Key('end_stream')));
    await t.pumpAndSettle();
  });
}
