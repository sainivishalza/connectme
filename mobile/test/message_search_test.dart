import 'package:connectme/features/chat/chat_providers.dart';
import 'package:connectme/features/chat/crypto/secret_store.dart';
import 'package:connectme/features/chat/data/chat_models.dart';
import 'package:connectme/features/chat/data/chat_repository.dart';
import 'package:connectme/features/chat/data/message_cache.dart';
import 'package:connectme/features/chat/data/message_search.dart';
import 'package:connectme/features/chat/screens/message_search_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';

ChatMessage m(String id, String text, {bool mine = false, DateTime? at, int burn = 0, DateTime? read, MsgKind kind = MsgKind.text, String? ctl, String? target}) => ChatMessage(
      id: id, fromMe: mine, kind: kind, text: text, createdAt: at ?? DateTime(2026, 5, 1, 12), burnAfterS: burn, readAt: read, ctl: ctl, ctlTarget: target);

Future<MessageCache> cacheWith(Map<String, List<ChatMessage>> convs) async {
  final cache = MessageCache(MemorySecretStore(), 'me');
  for (final e in convs.entries) {
    await cache.save(e.key, {for (final x in e.value) x.id: x});
  }
  return cache;
}

void main() {
  test('finds text ignoring case and accents, newest first, across conversations', () async {
    final cache = await cacheWith({
      'a': [m('1', 'Meet at the Café at 8', at: DateTime(2026, 5, 1)), m('2', 'nothing here', at: DateTime(2026, 5, 2))],
      'b': [m('3', 'best CAFE in town?', at: DateTime(2026, 5, 3), mine: true)],
    });
    final hits = await MessageSearch(cache).search('cafe', {'a': 'Ana', 'b': 'Bea'});
    expect(hits.map((h) => h.message.id), ['3', '1']);
    expect(hits.first.convTitle, 'Bea');
    final h = hits.last;
    expect(h.message.text.substring(h.start, h.end), 'Café', reason: 'the match positions line up with the original text');
  });

  test('short queries find nothing; photos, control messages, unsent and burned messages are skipped', () async {
    final cache = await cacheWith({
      'a': [
        m('1', 'hello there'),
        m('2', 'hello photo', kind: MsgKind.image),
        m('3', 'hello gone'),
        m('4', '', kind: MsgKind.control, ctl: 'del', target: '3', mine: false),
        m('5', 'hello burned', burn: 5, read: DateTime(2026, 5, 1, 11)),
      ],
    });
    final s = MessageSearch(cache);
    expect(await s.search('h', {'a': 'Ana'}), isEmpty);
    expect(await s.search('  ', {'a': 'Ana'}), isEmpty);
    // m3 is "unsent" by a notice from the *other* person, but it was theirs, so it is hidden;
    // the notice above came from them and m3 is theirs.
    final ids = (await s.search('hello', {'a': 'Ana'}, now: DateTime(2026, 5, 1, 13))).map((x) => x.message.id).toList();
    expect(ids, ['1']);
  });

  test('only the requested conversations are searched', () async {
    final cache = await cacheWith({'a': [m('1', 'secret plan')], 'b': [m('2', 'secret recipe')]});
    final hits = await MessageSearch(cache).search('secret', {'b': 'Bea'});
    expect(hits.map((h) => h.convId), ['b']);
  });

  testWidgets('the search screen finds messages, highlights the match and opens the chat at that message', (t) async {
    final cache = await cacheWith({
      'conv-1': [m('x1', 'Let us try the new ramen place on Friday', at: DateTime(2026, 5, 1))],
    });
    final router = GoRouter(routes: [
      GoRoute(path: '/', builder: (_, _) => const MessageSearchScreen()),
      GoRoute(path: '/chats/:id', builder: (_, s) => Scaffold(body: Text('CHAT ${s.pathParameters['id']} focus=${s.uri.queryParameters['focus']}'))),
    ]);
    await t.pumpWidget(ProviderScope(
      overrides: [
        messageCacheProvider.overrideWithValue(cache),
        conversationsProvider.overrideWith((ref) async => [Conversation(id: 'conv-1', title: 'Ana', subtitle: '', isGroup: false)]),
      ],
      child: MaterialApp.router(routerConfig: router),
    ));
    await t.pumpAndSettle();
    expect(find.textContaining('end-to-end encrypted'), findsOneWidget);
    await t.enterText(find.byKey(const Key('search_field')), 'ramen');
    await t.pump(const Duration(milliseconds: 400));
    await t.pumpAndSettle();
    expect(find.text('Ana'), findsOneWidget);
    expect(find.byKey(const Key('hit_0')), findsOneWidget);
    await t.tap(find.byKey(const Key('hit_0')));
    await t.pumpAndSettle();
    expect(find.text('CHAT conv-1 focus=x1'), findsOneWidget);
  });

  testWidgets('no matches says so, and searching inside one chat returns the message id', (t) async {
    final cache = await cacheWith({'conv-1': [m('x1', 'ramen on friday')], 'conv-2': [m('y1', 'ramen elsewhere')]});
    String? result;
    final router = GoRouter(routes: [
      GoRoute(
        path: '/',
        builder: (c, _) => Scaffold(body: TextButton(onPressed: () async => result = await c.push<String>('/s'), child: const Text('go'))),
      ),
      GoRoute(path: '/s', builder: (_, _) => const MessageSearchScreen(convId: 'conv-1')),
    ]);
    await t.pumpWidget(ProviderScope(
      overrides: [
        messageCacheProvider.overrideWithValue(cache),
        conversationsProvider.overrideWith((ref) async => [
              Conversation(id: 'conv-1', title: 'Ana', subtitle: '', isGroup: false),
              Conversation(id: 'conv-2', title: 'Bea', subtitle: '', isGroup: false),
            ]),
      ],
      child: MaterialApp.router(routerConfig: router),
    ));
    await t.tap(find.text('go'));
    await t.pumpAndSettle();
    await t.enterText(find.byKey(const Key('search_field')), 'zzz');
    await t.pump(const Duration(milliseconds: 400));
    await t.pumpAndSettle();
    expect(find.byKey(const Key('no_results')), findsOneWidget);
    await t.enterText(find.byKey(const Key('search_field')), 'ramen');
    await t.pump(const Duration(milliseconds: 400));
    await t.pumpAndSettle();
    expect(find.byKey(const Key('hit_0')), findsOneWidget);
    expect(find.byKey(const Key('hit_1')), findsNothing, reason: 'the other conversation is not searched');
    await t.tap(find.byKey(const Key('hit_0')));
    await t.pumpAndSettle();
    expect(result, 'x1');
  });
}
