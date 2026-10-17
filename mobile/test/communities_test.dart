import 'package:connectme/core/providers.dart';
import 'package:connectme/features/communities/screens/communities_screen.dart';
import 'package:connectme/features/communities/communities_repository.dart';
import 'package:connectme/features/social/screens/feed_screen.dart';
import 'package:connectme/features/social/social_repository.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';

import 'helpers.dart';
import 'social_test.dart' show postJson;

Map<String, dynamic> groupJson(String id, {String name = 'Hikers', String kind = 'interest', bool joined = false, bool owner = false, int members = 3}) => {
      'id': id,
      'name': name,
      'kind': kind,
      'description': 'Weekend trails',
      'tag': 'hiking',
      'members': members,
      'joined': joined,
      'owner': owner,
      'distance': 'within 5 km',
    };

Future<FakeAdapter> pump(WidgetTester t, Handler h) async {
  final client = fakeClient(h);
  final router = GoRouter(initialLocation: '/communities', routes: [
    GoRoute(path: '/communities', builder: (_, _) => const CommunitiesScreen()),
    GoRoute(path: '/community/:id', builder: (_, s) => FeedScreen(kind: PostKind.community, community: s.extra! as Community)),
    GoRoute(path: '/profile/edit', builder: (_, _) => const Scaffold(body: Text('edit'))),
  ]);
  await t.pumpWidget(ProviderScope(
    overrides: [apiClientProvider.overrideWithValue(client)],
    child: MaterialApp.router(routerConfig: router),
  ));
  await t.pumpAndSettle();
  return client.dio.httpClientAdapter as FakeAdapter;
}

void main() {
  testWidgets('lists nearby groups, joining opens the group feed', (t) async {
    await pump(t, (o) async {
      if (o.path == '/communities/nearby') {
        return Reply(200, {
          'communities': [groupJson('g1'), groupJson('h1', name: 'Hotel Aurora guests', kind: 'hotel', joined: true)],
        });
      }
      if (o.path == '/communities/g1/join') return Reply(200, groupJson('g1', joined: true, members: 4));
      if (o.path == '/communities/g1/posts') return Reply(200, {'posts': [postJson('p1', kind: 'community', body: 'Trail on Sunday?')]});
      return Reply(404);
    });
    expect(find.text('Hikers'), findsOneWidget);
    expect(find.text('Hotel Aurora guests'), findsOneWidget);
    expect(find.byKey(const Key('join_g1')), findsOneWidget);
    expect(find.byKey(const Key('join_h1')), findsNothing);

    await t.tap(find.byKey(const Key('join_g1')));
    await t.pumpAndSettle();
    expect(find.text('Trail on Sunday?'), findsOneWidget);
  });

  testWidgets('empty state invites starting a group, creating posts the form', (t) async {
    Map<String, dynamic>? sent;
    await pump(t, (o) async {
      if (o.path == '/communities/nearby') return Reply(200, {'communities': []});
      if (o.path == '/communities/' && o.method == 'POST') {
        sent = Map<String, dynamic>.from(o.data as Map);
        return Reply(201, groupJson('g2', name: 'Chess', joined: true, owner: true, members: 1));
      }
      if (o.path == '/communities/g2/posts') return Reply(200, {'posts': []});
      return Reply(404);
    });
    expect(find.text('No groups near you yet'), findsOneWidget);
    await t.tap(find.byKey(const Key('new_community')));
    await t.pumpAndSettle();
    await t.enterText(find.byKey(const Key('community_name')), 'Chess');
    await t.enterText(find.byKey(const Key('community_tag')), 'games');
    await t.tap(find.byKey(const Key('community_create')));
    await t.pumpAndSettle();
    expect(sent?['name'], 'Chess');
    expect(sent?['tag'], 'games');
    expect(find.text('No posts yet'), findsOneWidget);
  });

  testWidgets('a missing location explains what to do', (t) async {
    await pump(t, (o) async => Reply(422, {'error': 'set your location first'}));
    expect(find.text('Share your location first'), findsOneWidget);
  });
}
