import 'package:connectme/core/providers.dart';
import 'package:connectme/features/calls/call_media.dart';
import 'package:connectme/features/calls/calls_repository.dart';
import 'package:connectme/features/calls/incoming_call_watcher.dart';
import 'package:connectme/features/calls/screens/call_screen.dart';
import 'package:connectme/features/calls/screens/incoming_call_screen.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';

import 'helpers.dart';

class FakeCallMedia implements CallMedia {
  final joined = ValueNotifier<bool>(false);
  String? url, token;
  bool? video;
  bool? mic, cam;
  bool disconnected = false;

  @override
  Future<void> connect(String url, String token, {required bool video}) async {
    this.url = url;
    this.token = token;
    this.video = video;
  }

  @override
  ValueListenable<bool> get remoteJoined => joined;
  @override
  Widget remoteView() => const ColoredBox(color: Colors.black);
  @override
  Widget localView() => const ColoredBox(color: Colors.grey);
  @override
  Future<void> setMic(bool on) async => mic = on;
  @override
  Future<void> setCamera(bool on) async => cam = on;
  @override
  Future<void> disconnect() async => disconnected = true;
}

Map<String, dynamic> callJson({String status = 'ringing', bool video = false, bool mine = true}) => {
      'id': 'c1',
      'match_id': 'm1',
      'video': video,
      'status': status,
      'mine': mine,
      'peer': {'ref': 'r', 'anonymous': true, 'mode': 'incognito'},
    };

Map<String, dynamic> started({String status = 'ringing', bool video = false, bool mine = true}) => {
      'call': callJson(status: status, video: video, mine: mine),
      'connect': {'url': 'wss://lk.example', 'token': 'TOK', 'room': 'call-c1'},
    };

Future<(FakeAdapter, FakeCallMedia)> pumpCall(WidgetTester t, Handler h, {bool video = false, bool mine = true}) async {
  final client = fakeClient(h);
  final adapter = client.dio.httpClientAdapter as FakeAdapter;
  final media = FakeCallMedia();
  final s = StartedCall.fromJson(started(video: video, mine: mine));
  await t.pumpWidget(ProviderScope(
    overrides: [
      apiClientProvider.overrideWithValue(client),
      callMediaFactoryProvider.overrideWithValue(() => media),
    ],
    child: MaterialApp(
      home: Builder(
        builder: (c) => Scaffold(
          body: TextButton(
            key: const Key('open'),
            onPressed: () => Navigator.of(c).push(MaterialPageRoute<void>(
              builder: (_) => CallScreen(started: s, pollInterval: const Duration(milliseconds: 100)),
            )),
            child: const Text('open'),
          ),
        ),
      ),
    ),
  ));
  await t.tap(find.byKey(const Key('open')));
  await t.pump();
  await t.pump(const Duration(milliseconds: 50));
  return (adapter, media);
}

void main() {
  testWidgets('placing a call joins the room, shows Calling, then the clock once answered', (t) async {
    var status = 'ringing';
    final (_, media) = await pumpCall(t, (o) async {
      if (o.path == '/calls/c1') return Reply(200, callJson(status: status));
      return Reply(404, {'error': 'nope'});
    });
    expect((media.url, media.token, media.video), ('wss://lk.example', 'TOK', false));
    expect(find.text('Calling…'), findsOneWidget);

    status = 'active';
    await t.pump(const Duration(milliseconds: 150));
    await t.pump(const Duration(seconds: 3));
    expect(find.text('00:03'), findsOneWidget, reason: 'the clock starts once the other person answers');
    await t.tap(find.byKey(const Key('call_hangup')));
    await t.pumpAndSettle();
  });

  testWidgets('hanging up ends the call on the server, leaves the room and closes the screen', (t) async {
    final (adapter, media) = await pumpCall(t, (o) async {
      if (o.path == '/calls/c1/end') return Reply(204);
      if (o.path == '/calls/c1') return Reply(200, callJson());
      return Reply(404, {'error': 'nope'});
    });
    await t.tap(find.byKey(const Key('call_hangup')));
    await t.pumpAndSettle();
    expect(adapter.requests.any((r) => r.path == '/calls/c1/end' && r.method == 'POST'), true);
    expect(media.disconnected, true);
    expect(find.byKey(const Key('call_hangup')), findsNothing, reason: 'screen closed');
  });

  testWidgets('a declined call closes the screen by itself', (t) async {
    await pumpCall(t, (o) async {
      if (o.path == '/calls/c1/end') return Reply(204);
      if (o.path == '/calls/c1') return Reply(200, callJson(status: 'declined'));
      return Reply(404, {'error': 'nope'});
    });
    await t.pump(const Duration(milliseconds: 250));
    await t.pumpAndSettle();
    expect(find.byKey(const Key('call_status')), findsNothing);
  });

  testWidgets('mute and camera controls reach the media layer (camera only on video calls)', (t) async {
    final (_, media) = await pumpCall(t, (o) async {
      if (o.path == '/calls/c1/end') return Reply(204);
      return Reply(200, callJson(video: true));
    }, video: true);
    expect(media.video, true);
    await t.tap(find.byKey(const Key('call_mute')));
    await t.pump();
    expect(media.mic, false);
    await t.tap(find.byKey(const Key('call_camera')));
    await t.pump();
    expect(media.cam, false);
    await t.tap(find.byKey(const Key('call_hangup')));
    await t.pumpAndSettle();
  });

  testWidgets('a voice call has no camera button', (t) async {
    await pumpCall(t, (o) async => o.path == '/calls/c1/end' ? Reply(204) : Reply(200, callJson()));
    expect(find.byKey(const Key('call_camera')), findsNothing);
    await t.tap(find.byKey(const Key('call_hangup')));
    await t.pumpAndSettle();
  });

  group('incoming', () {
    Future<(FakeAdapter, GoRouter)> pumpIncoming(WidgetTester t, Handler h) async {
      final client = fakeClient(h);
      final adapter = client.dio.httpClientAdapter as FakeAdapter;
      final router = GoRouter(initialLocation: '/in', routes: [
        GoRoute(path: '/in', builder: (_, _) => IncomingCallScreen(call: CallInfo.fromJson(callJson(mine: false)))),
        GoRoute(path: '/call', builder: (_, s) => Scaffold(body: Text('in call ${(s.extra! as StartedCall).call.id}', key: const Key('in_call')))),
      ]);
      await t.pumpWidget(ProviderScope(
        overrides: [apiClientProvider.overrideWithValue(client)],
        child: MaterialApp.router(routerConfig: router),
      ));
      await t.pump();
      return (adapter, router);
    }

    testWidgets('accepting answers the call and opens it', (t) async {
      final (adapter, _) = await pumpIncoming(t, (o) async {
        if (o.path == '/calls/c1/accept') return Reply(200, started(status: 'active', mine: false));
        return Reply(404, {'error': 'nope'});
      });
      expect(find.text('Anonymous'), findsOneWidget);
      expect(find.text('Incoming voice call'), findsOneWidget);
      await t.tap(find.byKey(const Key('call_accept')));
      await t.pumpAndSettle();
      expect(adapter.requests.any((r) => r.path == '/calls/c1/accept'), true);
      expect(find.text('in call c1'), findsOneWidget);
    });

    testWidgets('declining tells the server', (t) async {
      final (adapter, _) = await pumpIncoming(t, (o) async => o.path == '/calls/c1/decline' ? Reply(204) : Reply(404, {'error': 'nope'}));
      await t.tap(find.byKey(const Key('call_decline')));
      await t.pump(const Duration(milliseconds: 50));
      expect(adapter.requests.any((r) => r.path == '/calls/c1/decline' && r.method == 'POST'), true);
    });
  });

  group('watcher', () {
    Future<(FakeAdapter, List<String>)> pumpWatcher(WidgetTester t, Handler h) async {
      final client = fakeClient(h);
      final adapter = client.dio.httpClientAdapter as FakeAdapter;
      final opened = <String>[];
      final router = GoRouter(initialLocation: '/home', routes: [
        GoRoute(
          path: '/home',
          builder: (_, _) => const IncomingCallWatcher(interval: Duration(milliseconds: 100), child: Scaffold(body: Text('home'))),
        ),
        GoRoute(
          path: '/call/incoming',
          builder: (_, s) {
            opened.add((s.extra! as CallInfo).id);
            return const Scaffold(body: Text('ringing', key: Key('ringing')));
          },
        ),
      ]);
      await t.pumpWidget(ProviderScope(
        overrides: [apiClientProvider.overrideWithValue(client)],
        child: MaterialApp.router(routerConfig: router),
      ));
      return (adapter, opened);
    }

    testWidgets('opens the incoming call screen once per call', (t) async {
      final (_, opened) = await pumpWatcher(t, (o) async => Reply(200, {'call': callJson(mine: false)}));
      await t.pump(const Duration(milliseconds: 250));
      await t.pumpAndSettle();
      expect(find.byKey(const Key('ringing')), findsOneWidget);
      await t.pump(const Duration(milliseconds: 500));
      expect(opened, ['c1'], reason: 'the same call must not open the screen again');
    });

    testWidgets('stops polling when the server has calls turned off', (t) async {
      final (adapter, _) = await pumpWatcher(t, (o) async => Reply(404, {'error': 'not found'}));
      await t.pump(const Duration(milliseconds: 250));
      final n = adapter.requests.length;
      await t.pump(const Duration(seconds: 1));
      expect(adapter.requests.length, n, reason: 'no more requests after a 404');
    });
  });
}
