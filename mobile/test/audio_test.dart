import 'package:connectme/core/providers.dart';
import 'package:connectme/features/audio/audio_repository.dart';
import 'package:connectme/features/audio/screens/audio_list_screen.dart';
import 'package:connectme/features/audio/screens/audio_room_screen.dart';
import 'package:connectme/features/calls/call_media.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';

import 'calls_test.dart' show FakeCallMedia;
import 'helpers.dart';

Map<String, dynamic> member(String id, String label, String role, {bool hand = false, bool me = false}) =>
    {'id': id, 'label': label, 'role': role, 'hand_raised': hand, 'me': me};

Map<String, dynamic> stateJson(String myRole, List<Map<String, dynamic>> members) => {
      'room': {'id': 'r1', 'title': 'Sunday jazz', 'anonymous': false, 'speakers': 1, 'listeners': 1, 'mine': myRole == 'host'},
      'members': members,
      'me': {'id': 'm-me', 'role': myRole},
    };

Map<String, dynamic> connectJson(String role) => {
      'url': 'wss://lk.example',
      'token': 'TOK-$role',
      'room': 'r1',
      'can_publish': role != 'listener',
      'me': {'id': 'm-me', 'role': role},
    };

final medias = <FakeCallMedia>[];

Future<FakeAdapter> pumpRoom(WidgetTester t, Handler h, {String? joinAs}) async {
  medias.clear();
  final client = fakeClient(h);
  final router = GoRouter(initialLocation: '/audio/r1', routes: [
    GoRoute(path: '/audio/:id', builder: (_, s) => AudioRoomScreen(roomId: s.pathParameters['id']!, pollInterval: const Duration(seconds: 1))),
  ]);
  await t.pumpWidget(ProviderScope(
    overrides: [
      apiClientProvider.overrideWithValue(client),
      callMediaFactoryProvider.overrideWithValue(() {
        final m = FakeCallMedia();
        medias.add(m);
        return m;
      }),
    ],
    child: MaterialApp.router(routerConfig: router),
  ));
  await t.pump();
  await t.pump(const Duration(milliseconds: 50));
  return client.dio.httpClientAdapter as FakeAdapter;
}

void main() {
  testWidgets('a listener sees the stage, raises a hand, and cannot manage others', (t) async {
    bool? hand;
    await pumpRoom(t, (o) async {
      if (o.path == '/audio/rooms/r1/join') return Reply(200, connectJson('listener'));
      if (o.path == '/audio/rooms/r1/hand') {
        hand = (o.data as Map)['up'] as bool;
        return Reply(204);
      }
      if (o.path == '/audio/rooms/r1') {
        return Reply(200, stateJson('listener', [member('h', 'Hostess', 'host'), member('m-me', 'Quiet Otter 12', 'listener', me: true, hand: hand ?? false)]));
      }
      return Reply(404);
    });
    expect(find.text('Sunday jazz'), findsOneWidget);
    expect(find.text('Hostess'), findsOneWidget);
    expect(find.text('Quiet Otter 12 (you)'), findsOneWidget);
    // Connected without publishing.
    expect(medias.single.token, 'TOK-listener');
    expect(medias.single.mic, isFalse);
    expect(find.byKey(const Key('mic_toggle')), findsNothing);

    await t.tap(find.byKey(const Key('raise_hand')));
    await t.pump();
    await t.pump(const Duration(milliseconds: 50));
    expect(hand, isTrue);
    expect(find.text('Lower hand'), findsOneWidget);

    // Tapping another person does nothing for a listener.
    await t.tap(find.byKey(const Key('member_h')));
    await t.pump();
    expect(find.byKey(const Key('kick')), findsNothing);
    await t.pumpWidget(const SizedBox());
  });

  testWidgets('the host invites a raised hand to speak', (t) async {
    String? action;
    await pumpRoom(t, (o) async {
      if (o.path == '/audio/rooms/r1/join') return Reply(200, connectJson('host'));
      if (o.path == '/audio/rooms/r1/members/g1') {
        action = (o.data as Map)['action'] as String;
        return Reply(204);
      }
      if (o.path == '/audio/rooms/r1') {
        return Reply(200, stateJson('host', [member('m-me', 'Hostess', 'host', me: true), member('g1', 'Brave Fox 41', 'listener', hand: true)]));
      }
      return Reply(404);
    });
    expect(find.byKey(const Key('hand_up')), findsOneWidget);
    expect(find.byKey(const Key('mic_toggle')), findsOneWidget);
    expect(find.byKey(const Key('end_audio')), findsOneWidget);

    await t.tap(find.byKey(const Key('member_g1')));
    await t.pump();
    await t.pump(const Duration(milliseconds: 300));
    await t.tap(find.byKey(const Key('promote')));
    await t.pump();
    await t.pump(const Duration(milliseconds: 50));
    expect(action, 'promote');
    await t.pumpWidget(const SizedBox());
  });

  testWidgets('being promoted reconnects with a speaking token', (t) async {
    var role = 'listener';
    await pumpRoom(t, (o) async {
      if (o.path == '/audio/rooms/r1/join') return Reply(200, connectJson(role));
      if (o.path == '/audio/rooms/r1') {
        return Reply(200, stateJson(role, [member('h', 'Hostess', 'host'), member('m-me', 'Gentle Wren 33', role, me: true)]));
      }
      return Reply(404);
    });
    expect(medias, hasLength(1));
    expect(find.byKey(const Key('raise_hand')), findsOneWidget);

    role = 'speaker';
    await t.pump(const Duration(seconds: 1));
    await t.pump(const Duration(milliseconds: 100));
    expect(medias, hasLength(2));
    expect(medias.first.disconnected, isTrue);
    expect(medias.last.token, 'TOK-speaker');
    expect(medias.last.mic, isTrue);
    expect(find.byKey(const Key('mic_toggle')), findsOneWidget);
    await t.pumpWidget(const SizedBox());
  });

  testWidgets('a finished room says so and drops the connection', (t) async {
    var live = true;
    await pumpRoom(t, (o) async {
      if (o.path == '/audio/rooms/r1/join') return Reply(200, connectJson('listener'));
      if (o.path == '/audio/rooms/r1') {
        return live ? Reply(200, stateJson('listener', [member('h', 'Hostess', 'host')])) : Reply(410, {'error': 'this room has ended'});
      }
      return Reply(404);
    });
    live = false;
    await t.pump(const Duration(seconds: 1));
    await t.pump(const Duration(milliseconds: 100));
    expect(find.text('This room has ended'), findsOneWidget);
    expect(medias.single.disconnected, isTrue);
    await t.pumpWidget(const SizedBox());
  });

  testWidgets('starting a room from the list opens it as the host', (t) async {
    Map<String, dynamic>? sent;
    final client = fakeClient((o) async {
      if (o.path == '/audio/rooms' && o.method == 'GET') {
        return Reply(200, {
          'rooms': [
            {'id': 'r9', 'title': 'Poetry hour', 'anonymous': true, 'speakers': 2, 'listeners': 5, 'mine': false}
          ]
        });
      }
      if (o.path == '/audio/rooms' && o.method == 'POST') {
        sent = Map<String, dynamic>.from(o.data as Map);
        return Reply(201, {
          'room': {'id': 'r1', 'title': 'Sunday jazz', 'anonymous': true, 'mine': true},
          'connect': connectJson('host'),
        });
      }
      if (o.path == '/audio/rooms/r1') return Reply(200, stateJson('host', [member('m-me', 'Calm Heron 20', 'host', me: true)]));
      return Reply(404);
    });
    final router = GoRouter(initialLocation: '/audio', routes: [
      GoRoute(path: '/audio', builder: (_, _) => const AudioListScreen()),
      GoRoute(
        path: '/audio/:id',
        builder: (_, s) => AudioRoomScreen(
            roomId: s.pathParameters['id']!, started: s.extra is StartedAudio ? s.extra as StartedAudio : null, pollInterval: const Duration(seconds: 1)),
      ),
    ]);
    medias.clear();
    await t.pumpWidget(ProviderScope(
      overrides: [
        apiClientProvider.overrideWithValue(client),
        callMediaFactoryProvider.overrideWithValue(() {
          final m = FakeCallMedia();
          medias.add(m);
          return m;
        }),
      ],
      child: MaterialApp.router(routerConfig: router),
    ));
    await t.pumpAndSettle();
    expect(find.text('Poetry hour'), findsOneWidget);
    expect(find.text('2 on stage · 5 listening · anonymous'), findsOneWidget);

    await t.tap(find.byKey(const Key('start_audio')));
    await t.pumpAndSettle();
    await t.enterText(find.byKey(const Key('audio_title')), 'Sunday jazz');
    await t.tap(find.byKey(const Key('audio_anonymous')));
    await t.pump();
    await t.tap(find.byKey(const Key('audio_open')));
    await t.pump();
    await t.pump(const Duration(milliseconds: 500));
    await t.pump(const Duration(milliseconds: 500));
    expect(sent, {'title': 'Sunday jazz', 'anonymous': true});
    expect(medias.single.token, 'TOK-host');
    expect(medias.single.mic, isTrue);
    expect(find.byKey(const Key('end_audio')), findsOneWidget);
    await t.pumpWidget(const SizedBox());
  });
}
