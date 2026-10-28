import 'package:connectme/core/models.dart';
import 'package:connectme/core/providers.dart';
import 'package:connectme/features/discovery/widgets/profile_card.dart';
import 'package:connectme/features/follows/follow_button.dart';
import 'package:connectme/features/follows/screens/follows_screen.dart';
import 'package:connectme/features/levels/screens/levels_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

Future<void> settle(WidgetTester t) async {
  await t.pumpAndSettle();
  await t.pump(const Duration(milliseconds: 100));
  await t.pumpAndSettle();
}

Future<FakeAdapter> show(WidgetTester t, Widget home, Handler h) async {
  t.view.physicalSize = const Size(800, 1800);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.reset);
  final client = fakeClient(h);
  await t.pumpWidget(ProviderScope(
    overrides: [apiClientProvider.overrideWithValue(client)],
    child: MaterialApp(home: home),
  ));
  await settle(t);
  return client.dio.httpClientAdapter as FakeAdapter;
}

PublicProfile person({bool anonymous = false, int charm = 0, int followers = 0, bool following = false}) => PublicProfile(
      ref: 'ref-1',
      anonymous: anonymous,
      mode: anonymous ? 'incognito' : 'open',
      displayName: anonymous ? '' : 'Asha',
      charmLevel: charm,
      followers: followers,
      following: following,
    );

void main() {
  testWidgets('levels screen shows both ladders and saves the visibility switches together', (t) async {
    var showSupporter = false;
    Object? saved;
    final a = await show(t, const LevelsScreen(), (o) async {
      if (o.path == '/levels/me' && o.method == 'GET') {
        return Reply(200, {
          'charm': {'level': 8, 'title': 'Rising', 'total': 2000, 'next_at': 2250, 'progress': 40},
          'supporter': {'level': 1, 'title': 'Newcomer', 'total': 20, 'next_at': 100, 'progress': 20},
          'show_charm': true,
          'show_supporter': showSupporter,
        });
      }
      if (o.path == '/levels/me/visibility') {
        saved = o.data;
        showSupporter = (o.data as Map)['show_supporter'] as bool;
        return Reply(204);
      }
      return Reply(404);
    });
    expect(find.text('Lv 8 Rising'), findsOneWidget);
    expect(find.text('Lv 1 Newcomer'), findsOneWidget);
    expect(find.text('2000 of 2250 coins to Lv 9'), findsOneWidget);
    expect(t.widget<SwitchListTile>(find.byKey(const Key('show_supporter'))).value, isFalse);

    await t.tap(find.byKey(const Key('show_supporter')));
    await settle(t);
    expect(saved, {'show_charm': true, 'show_supporter': true});
    expect(t.widget<SwitchListTile>(find.byKey(const Key('show_supporter'))).value, isTrue);
    expect(a.count('/levels/me/visibility'), 1);
  });

  testWidgets('leaderboards switch between hosts and supporters and show only levels and gift counts', (t) async {
    final a = await show(t, const LevelsScreen(), (o) async {
      if (o.path == '/levels/me') return Reply(200, {'charm': {'level': 1, 'title': 'Newcomer'}, 'supporter': {'level': 1, 'title': 'Newcomer'}, 'show_charm': true});
      if (o.path == '/levels/board/hosts') {
        return Reply(200, {
          'kind': 'hosts',
          'entries': [
            {'rank': 1, 'ref': 'r1', 'name': 'Star Host', 'gifts': 3, 'level': {'level': 4, 'title': 'Newcomer'}},
            {'rank': 2, 'ref': 'r2', 'name': 'Me', 'gifts': 1, 'me': true, 'level': {'level': 2, 'title': 'Newcomer'}},
          ],
          'my_rank': 2,
        });
      }
      if (o.path == '/levels/board/supporters') return Reply(200, {'kind': 'supporters', 'entries': []});
      return Reply(404);
    });
    await t.tap(find.byKey(const Key('tab_boards')));
    await settle(t);
    expect(find.text('Star Host'), findsOneWidget);
    expect(find.text('Lv 4 Newcomer - 3 gifts'), findsOneWidget);
    expect(find.text('Lv 2 Newcomer - 1 gift'), findsOneWidget);
    expect(find.text('You are #2'), findsOneWidget);
    expect(a.requests.last.queryParameters['period'], 'week');

    await t.tap(find.text('Top supporters'));
    await settle(t);
    expect(find.text('Nobody here yet'), findsOneWidget);
    await t.tap(find.text('Month'));
    await settle(t);
    expect(a.requests.last.path, '/levels/board/supporters');
    expect(a.requests.last.queryParameters['period'], 'month');
  });

  testWidgets('follow button follows, updates the count, and unfollows; anonymous profiles get no button', (t) async {
    final a = await show(t, Scaffold(body: FollowButton(profile: person(followers: 4))), (o) async => Reply(204));
    expect(find.text('4 followers'), findsOneWidget);
    await t.tap(find.byKey(const Key('follow')));
    await settle(t);
    expect(a.requests.last.method, 'POST');
    expect(a.requests.last.path, '/follows/ref-1');
    expect(find.text('5 followers'), findsOneWidget);
    expect(find.byKey(const Key('unfollow')), findsOneWidget);
    await t.tap(find.byKey(const Key('unfollow')));
    await settle(t);
    expect(a.requests.last.method, 'DELETE');
    expect(find.text('4 followers'), findsOneWidget);
  });

  testWidgets('a failed follow leaves the button and the count alone', (t) async {
    await show(t, Scaffold(body: FollowButton(profile: person(followers: 1))), (o) async => Reply(404, {'error': 'not found'}));
    await t.tap(find.byKey(const Key('follow')));
    await settle(t);
    expect(find.byKey(const Key('follow')), findsOneWidget);
    expect(find.text('1 follower'), findsOneWidget);
  });

  testWidgets('anonymous profiles show no follow button and no charm badge', (t) async {
    await show(t, Scaffold(body: FollowButton(profile: person(anonymous: true))), (o) async => Reply(204));
    expect(find.byKey(const Key('follow')), findsNothing);
    expect(find.byKey(const Key('follower_count')), findsNothing);
  });

  testWidgets('the profile card shows the charm badge from level 2 only', (t) async {
    await show(t, Scaffold(body: ProfileCard(profile: person(charm: 7))), (o) async => Reply(404));
    expect(find.text('Lv 7'), findsOneWidget);
    await show(t, Scaffold(body: ProfileCard(profile: person(charm: 0))), (o) async => Reply(404));
    expect(find.byKey(const Key('charm_badge')), findsNothing);
  });

  testWidgets('following screen lists people and an empty state for followers', (t) async {
    await show(t, const FollowsScreen(), (o) async {
      if (o.path == '/follows/following') {
        return Reply(200, {
          'profiles': [
            {'ref': 'r9', 'anonymous': false, 'mode': 'open', 'display_name': 'Mina', 'charm_level': 5}
          ]
        });
      }
      return Reply(200, {'profiles': []});
    });
    expect(find.text('Mina'), findsOneWidget);
    expect(find.text('Charm Lv 5'), findsOneWidget);
    await t.tap(find.byKey(const Key('tab_followers')));
    await settle(t);
    expect(find.text('No followers yet'), findsOneWidget);
  });
}
