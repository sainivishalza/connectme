import 'package:connectme/core/models.dart';
import 'package:connectme/core/providers.dart';
import 'package:connectme/features/chat/data/chat_models.dart';
import 'package:connectme/features/chat/screens/chat_screen.dart';
import 'package:connectme/features/discovery/discovery_repository.dart';
import 'package:connectme/features/discovery/screens/discover_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

void main() {
  test('swipe results, likes-you and matches carry the notes', () async {
    final client = fakeClient((o) async {
      if (o.path == '/matching/swipe') return Reply(200, {'matched': true, 'match_id': 'm1', 'their_comment': 'Love your trail photo'});
      if (o.path == '/matching/likes-you') {
        return Reply(200, {
          'count': 1,
          'locked': false,
          'profiles': [
            {'ref': 'r1', 'mode': 'open', 'anonymous': false, 'display_name': 'Ana', 'tags': <String>[]}
          ],
          'comments': {'r1': 'Your hiking photo made me smile'},
        });
      }
      return Reply(404);
    });
    final repo = DiscoveryRepository(client);
    final r = await repo.swipe('r1', 'like', comment: 'hello');
    expect(r.matched, isTrue);
    expect(r.theirComment, 'Love your trail photo');
    expect((client.dio.httpClientAdapter as FakeAdapter).requests.single.data, {'target_ref': 'r1', 'action': 'like', 'comment': 'hello'});
    final likes = await repo.likesYou();
    expect(likes.comments['r1'], 'Your hiking photo made me smile');
    final m = MatchView.fromJson({
      'match_id': 'm', 'mode': 'open', 'my_comment': 'Hi there', 'their_comment': 'Hello!', 'profile': {'ref': 'r', 'mode': 'open', 'anonymous': false},
    });
    expect((m.myComment, m.theirComment), ('Hi there', 'Hello!'));
  });

  testWidgets('Likes you shows the note above the person who wrote it', (t) async {
    final client = fakeClient((o) async {
      if (o.path == '/matching/likes-you') {
        return Reply(200, {
          'count': 2,
          'locked': false,
          'profiles': [
            {'ref': 'r1', 'mode': 'open', 'anonymous': false, 'display_name': 'Ana', 'tags': <String>[]},
            {'ref': 'r2', 'mode': 'open', 'anonymous': false, 'display_name': 'Bea', 'tags': <String>[]},
          ],
          'comments': {'r1': 'Your hiking photo made me smile'},
        });
      }
      return Reply(404);
    });
    await t.pumpWidget(ProviderScope(overrides: [apiClientProvider.overrideWithValue(client)], child: const MaterialApp(home: Scaffold(body: LikesYouList()))));
    await t.pumpAndSettle();
    expect(find.text('Your hiking photo made me smile'), findsOneWidget);
    expect(find.byKey(const Key('like_note_bubble')), findsOneWidget, reason: 'only Ana wrote a note');
  });

  testWidgets('the chat shows how the match started, each note on its side', (t) async {
    Conversation conv({String mine = '', String theirs = ''}) =>
        Conversation(id: 'm', title: 'Ana', subtitle: '', isGroup: false, myOpeningNote: mine, theirOpeningNote: theirs);
    await t.pumpWidget(MaterialApp(home: Scaffold(body: OpeningNotes(conv: conv(mine: 'Hi there', theirs: 'Your hiking photo made me smile')))));
    expect(find.text('How it started'), findsOneWidget);
    expect(find.textContaining('Your hiking photo made me smile'), findsOneWidget);
    expect(find.textContaining('Hi there'), findsOneWidget);
    expect(find.textContaining('Ana: '), findsOneWidget);
    expect(find.textContaining('You: '), findsOneWidget);
  });

  testWidgets('Top picks lists people with a reason, and liking one removes it from the list', (t) async {
    t.view.physicalSize = const Size(800, 2400);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.reset);
    final client = fakeClient((o) async {
      if (o.path == '/matching/top-picks') {
        return Reply(200, {
          'picks': [
            {'why': 'You both like jazz', 'profile': {'ref': 'r1', 'mode': 'open', 'anonymous': false, 'display_name': 'Ana', 'tags': ['jazz']}},
            {'why': 'You are both looking for a long-term relationship', 'profile': {'ref': 'r2', 'mode': 'open', 'anonymous': false, 'display_name': 'Bea', 'tags': <String>[]}},
          ]
        });
      }
      if (o.path == '/matching/swipe') return Reply(200, {'matched': false});
      return Reply(404);
    });
    await t.pumpWidget(ProviderScope(overrides: [apiClientProvider.overrideWithValue(client)], child: const MaterialApp(home: Scaffold(body: TopPicksList()))));
    await t.pumpAndSettle();
    expect(find.text('You both like jazz'), findsOneWidget);
    expect(find.text('You are both looking for a long-term relationship'), findsOneWidget);
    await t.tap(find.byKey(const Key('like')).first);
    await t.pumpAndSettle();
    expect((client.dio.httpClientAdapter as FakeAdapter).requests.last.data, {'target_ref': 'r1', 'action': 'like'});
    expect(find.text('You both like jazz'), findsNothing);
    expect(find.text('You are both looking for a long-term relationship'), findsOneWidget);
  });
}
