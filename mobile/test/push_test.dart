import 'dart:async';

import 'package:connectme/core/providers.dart';
import 'package:connectme/features/push/push_registrar.dart';
import 'package:connectme/features/push/push_source.dart';
import 'package:connectme/features/push/screens/notifications_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_riverpod/misc.dart' show Override;
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';

import 'helpers.dart';

class FakePush implements PushTokenSource {
  FakePush({this.authorized = false, this.grant = true, this.tok = 'fcm-token-1234567890'});
  bool authorized;
  final bool grant;
  final String? tok;
  int prompts = 0;
  final refresh = StreamController<String>.broadcast();
  final opened = StreamController<PushTap>.broadcast();

  @override
  String get platform => 'android';
  @override
  bool get available => true;
  @override
  Future<bool> isAuthorized() async => authorized;
  @override
  Future<bool> requestPermission() async {
    prompts++;
    authorized = grant;
    return grant;
  }

  @override
  Future<String?> token() async => tok;
  @override
  Stream<String> get onTokenRefresh => refresh.stream;
  @override
  Stream<PushTap> get onOpened => opened.stream;
}

Handler api({int devices = 0, required List<String> log, Map<String, dynamic>? prefs}) => (o) async {
      log.add('${o.method} ${o.path}');
      if (o.path == '/push/prefs' && o.method == 'GET') {
        return Reply(200, {
          'prefs': prefs ?? {'messages': true, 'matches': true, 'calls': true, 'bookings': true},
          'devices': devices,
        });
      }
      if (o.path == '/push/prefs' && o.method == 'PUT') {
        prefs = Map<String, dynamic>.from(o.data as Map);
        return Reply(204);
      }
      if (o.path == '/push/devices') return Reply(204);
      return Reply(404);
    };

Future<FakeAdapter> pump(WidgetTester t, Handler h, Widget home, {List<Override> extra = const []}) async {
  final client = fakeClient(h);
  await t.pumpWidget(ProviderScope(
    overrides: [apiClientProvider.overrideWithValue(client), ...extra],
    child: MaterialApp(home: home),
  ));
  await t.pumpAndSettle();
  return client.dio.httpClientAdapter as FakeAdapter;
}

void main() {
  testWidgets('without a push channel the screen says so and still saves choices', (t) async {
    final log = <String>[];
    await pump(t, api(log: log), const NotificationsScreen());
    expect(find.byKey(const Key('push_unavailable')), findsOneWidget);
    expect(find.byKey(const Key('enable_push')), findsNothing);

    await t.tap(find.byKey(const Key('pref_messages')));
    await t.pumpAndSettle();
    expect(log, contains('PUT /push/prefs'));
    expect(t.widget<SwitchListTile>(find.byKey(const Key('pref_messages'))).value, isFalse);
    expect(t.widget<SwitchListTile>(find.byKey(const Key('pref_calls'))).value, isTrue);
  });

  testWidgets('turning notifications on asks permission once and registers the token', (t) async {
    final fake = FakePush();
    final log = <String>[];
    Map<String, dynamic>? sent;
    await pump(
      t,
      (o) async {
        if (o.path == '/push/devices') sent = Map<String, dynamic>.from(o.data as Map);
        return api(log: log)(o);
      },
      const NotificationsScreen(),
      extra: [pushTokenSourceProvider.overrideWithValue(fake)],
    );
    expect(find.byKey(const Key('enable_push')), findsOneWidget);
    await t.tap(find.byKey(const Key('enable_push')));
    await t.pumpAndSettle();
    expect(fake.prompts, 1);
    expect(sent, {'token': 'fcm-token-1234567890', 'platform': 'android'});
  });

  testWidgets('a refused permission is explained and registers nothing', (t) async {
    final fake = FakePush(grant: false);
    final log = <String>[];
    await pump(t, api(log: log), const NotificationsScreen(), extra: [pushTokenSourceProvider.overrideWithValue(fake)]);
    await t.tap(find.byKey(const Key('enable_push')));
    await t.pump();
    await t.pump(const Duration(milliseconds: 100));
    expect(find.textContaining('blocked'), findsOneWidget);
    expect(log.where((l) => l.contains('/push/devices')), isEmpty);
  });

  testWidgets('a failed save puts the switch back', (t) async {
    await pump(t, (o) async {
      if (o.path == '/push/prefs' && o.method == 'GET') {
        return Reply(200, {
          'prefs': {'messages': true, 'matches': true, 'calls': true, 'bookings': true},
          'devices': 0
        });
      }
      return Reply(500, {'error': 'boom'});
    }, const NotificationsScreen());
    await t.tap(find.byKey(const Key('pref_calls')));
    await t.pump();
    await t.pump(const Duration(milliseconds: 100));
    expect(t.widget<SwitchListTile>(find.byKey(const Key('pref_calls'))).value, isTrue);
  });

  testWidgets('the registrar registers silently when already allowed, follows token refresh, and opens taps', (t) async {
    final fake = FakePush(authorized: true);
    final tokens = <String>[];
    final client = fakeClient((o) async {
      if (o.path == '/push/devices') tokens.add((o.data as Map)['token'] as String);
      return Reply(204);
    });
    final router = GoRouter(routes: [
      GoRoute(path: '/', builder: (_, _) => const PushRegistrar(child: Scaffold(body: Text('home')))),
      GoRoute(path: '/chats/:id', builder: (_, s) => Scaffold(body: Text('chat ${s.pathParameters['id']}'))),
      GoRoute(path: '/events', builder: (_, _) => const Scaffold(body: Text('events'))),
    ]);
    await t.pumpWidget(ProviderScope(
      overrides: [apiClientProvider.overrideWithValue(client), pushTokenSourceProvider.overrideWithValue(fake)],
      child: MaterialApp.router(routerConfig: router),
    ));
    await t.pumpAndSettle();
    expect(fake.prompts, 0, reason: 'never prompts on its own');
    expect(tokens, ['fcm-token-1234567890']);

    fake.refresh.add('rotated-token-abcdefghij');
    await t.pumpAndSettle();
    expect(tokens.last, 'rotated-token-abcdefghij');

    fake.opened.add(const PushTap(kind: 'message', ref: 'm42'));
    await t.pumpAndSettle();
    expect(find.text('chat m42'), findsOneWidget);
    router.pop();
    await t.pumpAndSettle();
    fake.opened.add(const PushTap(kind: 'booking', ref: 'b1'));
    await t.pumpAndSettle();
    expect(find.text('events'), findsOneWidget);
  });

  testWidgets('the registrar stays quiet when notifications are not allowed', (t) async {
    final fake = FakePush(authorized: false);
    var calls = 0;
    final client = fakeClient((o) async {
      calls++;
      return Reply(204);
    });
    await t.pumpWidget(ProviderScope(
      overrides: [apiClientProvider.overrideWithValue(client), pushTokenSourceProvider.overrideWithValue(fake)],
      child: const MaterialApp(home: PushRegistrar(child: Scaffold(body: Text('home')))),
    ));
    await t.pumpAndSettle();
    expect(calls, 0);
    expect(fake.prompts, 0);
  });

  testWidgets('quiet hours: switch on, times shown, saved with the phone time zone offset', (t) async {
    t.view.physicalSize = const Size(800, 2200);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.reset);
    final log = <String>[];
    Object? sent;
    final a = await pump(t, (o) async {
      if (o.path == '/push/prefs' && o.method == 'PUT') sent = o.data;
      return api(log: log, prefs: {'messages': true, 'quiet': {'on': false, 'start': 1320, 'end': 480}})(o);
    }, const NotificationsScreen());
    expect(find.byKey(const Key('quiet_start')), findsNothing);
    await t.tap(find.byKey(const Key('pref_quiet')));
    await t.pumpAndSettle();
    expect(find.text('22:00'), findsOneWidget);
    expect(find.text('08:00'), findsOneWidget);
    final quiet = (sent! as Map)['quiet'] as Map;
    expect(quiet['on'], true);
    expect(quiet['start'], 1320);
    expect(quiet['tz_min'], DateTime.now().timeZoneOffset.inMinutes);
    expect(a.requests.where((r) => r.method == 'PUT').length, 1);
  });
}
