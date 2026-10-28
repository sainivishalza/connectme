import 'dart:convert';

import 'package:dio/dio.dart' show RequestOptions;

import 'package:connectme/core/providers.dart';
import 'package:connectme/features/discovery/screens/discover_screen.dart';
import 'package:connectme/shared/widgets/avatar.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';

import 'helpers.dart';

// A 1x1 transparent PNG, enough for Image.memory.
final Uint8List png = base64Decode('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==');

Map<String, dynamic> person(String ref, String name, {List<String> photos = const [], Map<String, dynamic>? details, bool verified = false}) => {
      'ref': ref,
      'mode': 'open',
      'anonymous': false,
      'display_name': name,
      'age': 30,
      'photos': photos,
      'tags': <String>[],
      'intro': 'hello from $name',
      'distance': 'within 2 km',
      'id_verified': ?(verified ? true : null),
      'details': ?details,
    };

Future<FakeAdapter> pumpDeck(WidgetTester t, Handler h) async {
  t.view.physicalSize = const Size(800, 1600);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.reset);
  final client = fakeClient(h);
  final router = GoRouter(routes: [
    GoRoute(path: '/', builder: (_, _) => const Scaffold(body: SwipeDeck())),
    GoRoute(path: '/chats/:id', builder: (_, s) => Scaffold(body: Text('CHAT ${s.pathParameters['id']}'))),
    GoRoute(path: '/profile/premium', builder: (_, _) => const Scaffold(body: Text('PLANS'))),
  ]);
  await t.pumpWidget(ProviderScope(
    overrides: [
      apiClientProvider.overrideWithValue(client),
      photoBytesProvider.overrideWith((ref, path) async => png),
    ],
    child: MaterialApp.router(routerConfig: router),
  ));
  await t.pumpAndSettle();
  return client.dio.httpClientAdapter as FakeAdapter;
}

Handler people(List<Map<String, dynamic>> list, {Future<Reply> Function(RequestOptions)? swipe}) => (o) async {
      if (o.path == '/users/nearby') return Reply(200, {'results': list});
      if (o.path.startsWith('/matching/')) return swipe == null ? Reply(200, {'matched': false}) : swipe(o);
      return Reply(404);
    };

/// Lets the animation finish, then the request it triggers, then the rebuild.
Future<void> settle(WidgetTester t) async {
  await t.pumpAndSettle();
  await t.pump(const Duration(milliseconds: 100));
  await t.pumpAndSettle();
}

void main() {
  testWidgets('dragging the card far right likes it; the next card comes up', (t) async {
    final a = await pumpDeck(t, people([person('a', 'Ana'), person('b', 'Bea')]));
    expect(find.text('Ana, 30'), findsOneWidget);
    await t.drag(find.byKey(const ValueKey('top_a')), const Offset(420, 20));
    await settle(t);
    expect(a.requests.last.data, {'target_ref': 'a', 'action': 'like'});
    expect(find.text('Ana, 30'), findsNothing);
    expect(find.text('Bea, 30'), findsOneWidget);
  });

  testWidgets('a short drag springs back and sends nothing; a left drag passes; an upward drag super likes', (t) async {
    final a = await pumpDeck(t, people([person('a', 'Ana'), person('b', 'Bea'), person('c', 'Cleo')]));
    await t.drag(find.byKey(const ValueKey('top_a')), const Offset(40, 0));
    await settle(t);
    expect(a.requests.where((r) => r.path.startsWith('/matching/')), isEmpty);
    expect(find.text('Ana, 30'), findsOneWidget);

    await t.drag(find.byKey(const ValueKey('top_a')), const Offset(-420, 0));
    await settle(t);
    expect(a.requests.last.data, {'target_ref': 'a', 'action': 'pass'});

    await t.drag(find.byKey(const ValueKey('top_b')), const Offset(0, -420));
    await settle(t);
    expect(a.requests.last.path, '/matching/superlike');
    expect(find.text('Cleo, 30'), findsOneWidget);
  });

  testWidgets('the buttons and the arrow keys do the same as dragging', (t) async {
    final a = await pumpDeck(t, people([person('a', 'Ana'), person('b', 'Bea'), person('c', 'Cleo')]));
    await t.tap(find.byKey(const Key('pass')));
    await settle(t);
    expect(a.requests.last.data, {'target_ref': 'a', 'action': 'pass'});
    await t.sendKeyEvent(LogicalKeyboardKey.arrowRight);
    await settle(t);
    expect(a.requests.last.data, {'target_ref': 'b', 'action': 'like'});
  });

  testWidgets('a refused swipe springs the card back and offers the upgrade', (t) async {
    await pumpDeck(t, people([person('a', 'Ana')], swipe: (o) async => Reply(402, {'error': 'daily like limit reached'})));
    await t.drag(find.byKey(const ValueKey('top_a')), const Offset(420, 0));
    await settle(t);
    expect(find.text('daily like limit reached'), findsOneWidget);
    expect(find.text('See plans'), findsOneWidget);
    await t.tap(find.text('Not now'));
    await t.pumpAndSettle();
    expect(find.text('Ana, 30'), findsOneWidget, reason: 'the card must come back');
  });

  testWidgets('photos flip with edge taps, bars show where you are, and the middle opens the full profile', (t) async {
    await pumpDeck(t, people([
      person('a', 'Ana', photos: ['/media/photos/1', '/media/photos/2', '/media/photos/3'], verified: true, details: {'goal': 'long_term', 'job_title': 'Nurse'}),
    ]));
    expect(find.byKey(const Key('photo_bar_0')), findsOneWidget);
    expect(find.byKey(const Key('photo_bar_2')), findsOneWidget);
    expect(find.byKey(const Key('verified_badge')), findsOneWidget);
    expect(find.text('Long-term relationship'), findsOneWidget);
    final card = find.byKey(const ValueKey('top_a'));
    final r = t.getRect(card);
    Color? bar(int i) => (t.widget<Container>(find.byKey(Key('photo_bar_$i'))).decoration as BoxDecoration).color;
    expect(bar(0), Colors.white);
    await t.tapAt(Offset(r.right - 10, r.center.dy));
    await t.pump();
    expect(bar(1), Colors.white);
    expect(bar(0), isNot(Colors.white));
    await t.tapAt(Offset(r.left + 10, r.center.dy));
    await t.pump();
    expect(bar(0), Colors.white);
    // Middle tap opens the sheet with the whole profile.
    await t.tapAt(r.center);
    await t.pumpAndSettle();
    expect(find.byKey(const Key('profile_sheet')), findsOneWidget);
    expect(find.text('Nurse'), findsWidgets);
  });

  testWidgets('a match opens the celebration; Say hi goes to the chat', (t) async {
    await pumpDeck(t, people([person('a', 'Ana')], swipe: (o) async => Reply(200, {'matched': true, 'match_id': 'm9'})));
    await t.tap(find.byKey(const Key('like')));
    await settle(t);
    expect(find.byKey(const Key('match_title')), findsOneWidget);
    expect(find.text('Ana likes you back'), findsOneWidget);
    await t.tap(find.byKey(const Key('match_say_hi')));
    await t.pumpAndSettle();
    expect(find.text('CHAT m9'), findsOneWidget);
  });

  testWidgets('rewind brings back the last pass; without Gold it offers the plans; it only undoes passes', (t) async {
    var gold = false;
    var rewinds = 0;
    final a = await pumpDeck(t, people([person('a', 'Ana'), person('b', 'Bea'), person('c', 'Cleo')], swipe: (o) async {
      if (o.path == '/matching/rewind') {
        rewinds++;
        return gold ? Reply(204) : Reply(402, {'error': 'rewind is a Gold feature'});
      }
      return Reply(200, {'matched': false});
    }));
    await t.tap(find.byKey(const Key('rewind')));
    await settle(t);
    expect(find.text('Nothing to rewind'), findsOneWidget);
    expect(rewinds, 0, reason: 'nothing was passed yet, so do not bother the server');

    await t.tap(find.byKey(const Key('pass')));
    await settle(t);
    expect(find.text('Bea, 30'), findsOneWidget);
    await t.tap(find.byKey(const Key('rewind')));
    await settle(t);
    expect(find.text('rewind is a Gold feature'), findsOneWidget);
    await t.tap(find.text('Not now'));
    await t.pumpAndSettle();
    expect(find.text('Bea, 30'), findsOneWidget);

    gold = true;
    await t.tap(find.byKey(const Key('rewind')));
    await settle(t);
    expect(find.text('Ana, 30'), findsOneWidget);
    expect(a.count('/matching/rewind'), 2);

    // After a like, the earlier pass can no longer be rewound.
    await t.tap(find.byKey(const Key('pass')));
    await settle(t);
    await t.tap(find.byKey(const Key('like')));
    await settle(t);
    await t.tap(find.byKey(const Key('rewind')));
    await settle(t);
    expect(find.text('Nothing to rewind'), findsOneWidget);
  });

  testWidgets('a note typed in the profile sheet is sent with the like', (t) async {
    final a = await pumpDeck(t, people([person('a', 'Ana'), person('b', 'Bea')]));
    await t.tapAt(t.getRect(find.byKey(const ValueKey('top_a'))).center);
    await t.pumpAndSettle();
    expect(t.widget<FilledButton>(find.byKey(const Key('send_like_note'))).onPressed, isNull, reason: 'empty note: nothing to send');
    await t.enterText(find.byKey(const Key('like_note')), '  Your hiking photo made me smile ');
    await t.pump();
    await t.ensureVisible(find.byKey(const Key('send_like_note')));
    await t.tap(find.byKey(const Key('send_like_note')));
    await settle(t);
    expect(a.requests.last.path, '/matching/swipe');
    expect(a.requests.last.data, {'target_ref': 'a', 'action': 'like', 'comment': 'Your hiking photo made me smile'});
    expect(find.text('Bea, 30'), findsOneWidget);
    // The next plain like carries no note.
    await t.tap(find.byKey(const Key('like')));
    await settle(t);
    expect(a.requests.last.data, {'target_ref': 'b', 'action': 'like'});
  });
}
