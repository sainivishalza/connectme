import 'package:connectme/features/chat/data/chat_models.dart';
import 'package:connectme/features/chat/widgets/message_bubble.dart';
import 'package:connectme/features/chat/widgets/message_menu.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

ChatMessage msg({bool mine = false, String text = 'See you at 8', String? replyTo, String quote = '', Map<String, String> reactions = const {}, MsgKind kind = MsgKind.text}) =>
    ChatMessage(id: 'm1', fromMe: mine, kind: kind, createdAt: DateTime(2026, 1, 1, 9, 5), text: text, replyToId: replyTo, replyQuote: quote, reactions: reactions);

Future<void> pumpBubble(WidgetTester t, ChatMessage m, {VoidCallback? onLong, VoidCallback? onDouble}) => t.pumpWidget(
    MaterialApp(home: Scaffold(body: Center(child: MessageBubble(message: m, onLongPress: onLong, onDoubleTap: onDouble)))));

void main() {
  testWidgets('a reply shows the quote above the text; a plain message does not', (t) async {
    await pumpBubble(t, msg(replyTo: 'x', quote: 'Dinner on Friday?'));
    expect(find.byKey(const Key('reply_quote')), findsOneWidget);
    expect(find.text('Dinner on Friday?'), findsOneWidget);
    expect(find.text('See you at 8'), findsOneWidget);
    await pumpBubble(t, msg());
    expect(find.byKey(const Key('reply_quote')), findsNothing);
  });

  testWidgets('reactions show under the bubble, theirs first; control messages draw nothing', (t) async {
    await pumpBubble(t, msg(reactions: {'me': '❤️', 'them': '😂'}));
    expect(find.text('😂 ❤️'), findsOneWidget);
    await pumpBubble(t, msg());
    expect(find.byKey(const Key('reactions')), findsNothing);
    await t.pumpWidget(MaterialApp(home: Scaffold(body: MessageBubble(message: msg(kind: MsgKind.control)))));
    expect(find.byType(Text), findsNothing);
  });

  testWidgets('long-press and double-tap are reported', (t) async {
    var long = 0, dbl = 0;
    await pumpBubble(t, msg(), onLong: () => long++, onDouble: () => dbl++);
    await t.longPress(find.text('See you at 8'));
    expect(long, 1);
    await t.tap(find.text('See you at 8'));
    await t.pump(const Duration(milliseconds: 40));
    await t.tap(find.text('See you at 8'));
    await t.pumpAndSettle();
    expect(dbl, 1);
  });

  testWidgets('the menu offers reactions and reply for anyone, copy for text, unsend only for my own messages', (t) async {
    MessageAction? chosen;
    Future<void> open(ChatMessage m) async {
      await t.pumpWidget(MaterialApp(
        home: Scaffold(
          body: Builder(
            builder: (c) => TextButton(onPressed: () async => chosen = await showMessageMenu(c, m), child: const Text('open')),
          ),
        ),
      ));
      await t.tap(find.text('open'));
      await t.pumpAndSettle();
    }

    await open(msg());
    expect(find.byKey(const Key('menu_reply')), findsOneWidget);
    expect(find.byKey(const Key('menu_copy')), findsOneWidget);
    expect(find.byKey(const Key('menu_unsend')), findsNothing, reason: 'you cannot unsend their message');
    await t.tap(find.byKey(const Key('react_😂')));
    await t.pumpAndSettle();
    expect((chosen as ReactAction).emoji, '😂');

    await open(msg(mine: true));
    expect(find.byKey(const Key('menu_unsend')), findsOneWidget);
    await t.tap(find.byKey(const Key('menu_unsend')));
    await t.pumpAndSettle();
    expect(chosen, isA<UnsendAction>());

    await open(msg(kind: MsgKind.image, text: ''));
    expect(find.byKey(const Key('menu_copy')), findsNothing, reason: 'nothing to copy from a photo');
    await t.tap(find.byKey(const Key('menu_reply')));
    await t.pumpAndSettle();
    expect(chosen, isA<ReplyAction>());
  });
}
