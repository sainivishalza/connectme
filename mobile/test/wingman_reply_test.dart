import 'package:connectme/features/chat/data/chat_models.dart';
import 'package:connectme/features/chat/widgets/wingman_reply_sheet.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

ChatMessage msg(String id, String text, {bool mine = false}) =>
    ChatMessage(id: id, fromMe: mine, kind: MsgKind.text, createdAt: DateTime.now(), text: text);

void main() {
  testWidgets('only ticked messages are shared, anonymised, and a tapped idea is returned', (t) async {
    t.view.physicalSize = const Size(800, 2000);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.reset);
    List<({bool mine, String text})>? sent;
    String? result;
    await t.pumpWidget(MaterialApp(
      home: Builder(
        builder: (c) => Scaffold(
          body: TextButton(
            onPressed: () async => result = await showModalBottomSheet<String>(
              context: c,
              isScrollControlled: true,
              builder: (_) => WingmanReplySheet(
                messages: [msg('1', 'old one'), msg('2', 'older'), msg('3', 'Bowling on Friday?'), msg('4', 'Maybe!', mine: true), msg('5', 'Who is in?')],
                load: (lines) async {
                  sent = lines;
                  return ['Count me in!', 'Which night?'];
                },
              ),
            ),
            child: const Text('open'),
          ),
        ),
      ),
    ));
    await t.tap(find.text('open'));
    await t.pumpAndSettle();
    expect(find.textContaining('not saved'), findsOneWidget);
    expect(sent, isNull); // nothing leaves until the button is tapped

    // The latest three are pre-ticked; untick one and tick an older one.
    await t.tap(find.byKey(const Key('share_5')));
    await t.tap(find.byKey(const Key('share_1')));
    await t.pump();
    await t.tap(find.byKey(const Key('get_reply_ideas')));
    await t.pumpAndSettle();
    expect(sent!.map((l) => l.text), ['old one', 'Bowling on Friday?', 'Maybe!']);
    expect(sent!.map((l) => l.mine), [false, false, true]);

    expect(find.textContaining('written by an AI'), findsOneWidget);
    await t.tap(find.text('Which night?'));
    await t.pumpAndSettle();
    expect(result, 'Which night?');
  });

  testWidgets('with nothing ticked the button stays disabled', (t) async {
    await t.pumpWidget(MaterialApp(home: Scaffold(body: WingmanReplySheet(messages: [msg('1', 'hello')], load: (_) async => []))));
    await t.tap(find.byKey(const Key('share_1')));
    await t.pump();
    final b = t.widget<FilledButton>(find.byKey(const Key('get_reply_ideas')));
    expect(b.onPressed, isNull);
  });
}
