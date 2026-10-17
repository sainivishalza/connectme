import 'package:connectme/core/providers.dart';
import 'package:connectme/features/forums/forums_repository.dart';
import 'package:connectme/features/forums/screens/board_screen.dart';
import 'package:connectme/features/forums/screens/forums_screen.dart';
import 'package:connectme/features/forums/screens/thread_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';

import 'helpers.dart';

Map<String, dynamic> threadJson(String id, {String title = 'How do I start a chat?', String author = 'Olive', bool mine = false, int replies = 0, String body = 'I freeze after the match.'}) => {
      'id': id,
      'board': 'dating-advice',
      'title': title,
      'body': body,
      'author': author,
      'replies': replies,
      'mine': mine,
      'created_at': DateTime.now().subtract(const Duration(hours: 2)).toUtc().toIso8601String(),
      'last_reply_at': DateTime.now().subtract(const Duration(minutes: 9)).toUtc().toIso8601String(),
    };

Map<String, dynamic> replyJson(int id, String body, {String author = 'Quiet Otter 12', bool mine = false}) => {
      'id': id,
      'body': body,
      'author': author,
      'mine': mine,
      'created_at': DateTime.now().subtract(const Duration(minutes: 20)).toUtc().toIso8601String(),
    };

Future<FakeAdapter> pumpForums(WidgetTester t, Handler h) async {
  final client = fakeClient(h);
  final router = GoRouter(initialLocation: '/forums', routes: [
    GoRoute(path: '/forums', builder: (_, _) => const ForumsScreen()),
    GoRoute(path: '/forums/:slug', builder: (_, s) => BoardScreen(board: s.extra! as Board)),
    GoRoute(path: '/thread/:id', builder: (_, s) => ThreadScreen(threadId: s.pathParameters['id']!)),
  ]);
  await t.pumpWidget(ProviderScope(
    overrides: [apiClientProvider.overrideWithValue(client)],
    child: MaterialApp.router(routerConfig: router),
  ));
  await t.pumpAndSettle();
  return client.dio.httpClientAdapter as FakeAdapter;
}

void main() {
  testWidgets('boards lead to threads, and a new thread opens', (t) async {
    Map? created;
    await pumpForums(t, (o) async {
      if (o.path == '/forums/boards') {
        return Reply(200, {
          'boards': [
            {'slug': 'dating-advice', 'title': 'Dating advice', 'description': 'Ask and answer.', 'kind': 'advice', 'threads': 1},
            {'slug': 'travel-tips', 'title': 'Travel tips', 'description': '', 'kind': 'travel', 'threads': 0},
          ]
        });
      }
      if (o.path == '/forums/boards/dating-advice/threads' && o.method == 'GET') {
        return Reply(200, {
          'threads': [threadJson('t1', replies: 2)]
        });
      }
      if (o.path == '/forums/boards/dating-advice/threads' && o.method == 'POST') {
        created = o.data as Map;
        return Reply(201, threadJson('t2', title: 'New question here', mine: true, author: 'Me'));
      }
      if (o.path == '/forums/threads/t2') return Reply(200, threadJson('t2', title: 'New question here', mine: true));
      if (o.path == '/forums/threads/t2/replies') return Reply(200, {'replies': []});
      return Reply(404);
    });
    expect(find.text('Dating advice'), findsOneWidget);
    expect(find.text('Travel tips'), findsOneWidget);
    await t.tap(find.byKey(const Key('board_dating-advice')));
    await t.pumpAndSettle();
    expect(find.text('How do I start a chat?'), findsOneWidget);
    expect(find.textContaining('2 replies'), findsOneWidget);

    await t.tap(find.byKey(const Key('new_thread')));
    await t.pumpAndSettle();
    await t.enterText(find.byKey(const Key('thread_title')), 'New question here');
    await t.enterText(find.byKey(const Key('thread_body')), 'What should I say first?');
    await t.tap(find.byKey(const Key('thread_post')));
    await t.pumpAndSettle();
    expect(created, {'title': 'New question here', 'body': 'What should I say first?'});
    expect(find.text('New question here'), findsOneWidget);
    expect(find.byKey(const Key('delete_thread')), findsOneWidget, reason: 'own thread opens with delete, not report');
  });

  testWidgets('a thread shows replies, takes a reply, and reports others', (t) async {
    var replies = [replyJson(1, 'Try asking about their tags.')];
    String? reason;
    String? posted;
    final adapter = await pumpForums(t, (o) async {
      if (o.path == '/forums/boards') return Reply(200, {'boards': []});
      if (o.path == '/forums/threads/t1') return Reply(200, threadJson('t1', replies: replies.length));
      if (o.path == '/forums/threads/t1/replies' && o.method == 'GET') return Reply(200, {'replies': replies});
      if (o.path == '/forums/threads/t1/replies' && o.method == 'POST') {
        posted = (o.data as Map)['body'] as String;
        replies = [...replies, replyJson(2, posted!, author: 'Me', mine: true)];
        return Reply(201, replyJson(2, posted!, mine: true));
      }
      if (o.path == '/forums/threads/t1/report' || o.path == '/forums/replies/1/report') {
        reason = (o.data as Map)['reason'] as String;
        return Reply(204);
      }
      return Reply(404);
    });
    // Open the thread directly.
    final router = GoRouter.of(t.element(find.byType(Scaffold).first));
    router.push('/thread/t1');
    await t.pumpAndSettle();

    expect(find.text('How do I start a chat?'), findsOneWidget);
    expect(find.text('Olive · 2 h ago'), findsOneWidget);
    expect(find.text('Try asking about their tags.'), findsOneWidget);
    expect(find.text('Quiet Otter 12'), findsOneWidget);
    expect(find.byKey(const Key('report_thread')), findsOneWidget);

    await t.enterText(find.byKey(const Key('reply_field')), 'Thanks, that helps.');
    await t.tap(find.byKey(const Key('reply_send')));
    await t.pumpAndSettle();
    expect(posted, 'Thanks, that helps.');
    expect(find.byKey(const Key('delete_reply_2')), findsOneWidget);

    await t.tap(find.byKey(const Key('report_reply_1')));
    await t.pumpAndSettle();
    await t.tap(find.byKey(const Key('report_harassment')));
    await t.pumpAndSettle();
    expect(reason, 'harassment');
    expect(adapter.requests.any((r) => r.path == '/forums/replies/1/report'), isTrue);
  });
}
