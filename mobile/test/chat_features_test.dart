import 'dart:convert';
import 'dart:typed_data';

import 'package:connectme/features/chat/chat_controller.dart';
import 'package:connectme/features/chat/data/chat_models.dart';
import 'package:flutter_test/flutter_test.dart';

import 'chat_rig.dart';

/// What the chat would show right now.
List<ChatMessage> shown(Rig rig) => rig.state.visible(DateTime.now());

void main() {
  test('a reply carries the id and a quote of the message, inside the encrypted payload', () async {
    final rig = await Rig.create();
    addTearDown(rig.container.dispose);
    rig.server.fromBob(await rig.bobSays('Dinner on Friday?'));
    await rig.load();
    final theirs = rig.state.messages.single;

    await rig.ctrl.sendText('Yes, 8pm works', replyTo: theirs);
    final stored = rig.server.stored.last;
    expect(utf8.decode(base64.decode(stored.ciphertext), allowMalformed: true).contains('Dinner'), isFalse, reason: 'the quote is encrypted');
    final p = MessagePayload.decode(await rig.bob.decryptDirect(conv, Uint8List.fromList(base64.decode(stored.ciphertext))))!;
    expect(p.text, 'Yes, 8pm works');
    expect(p.replyTo, theirs.id);
    expect(p.quote, 'Dinner on Friday?');
    final mine = shown(rig).last;
    expect((mine.replyToId, mine.replyQuote), (theirs.id, 'Dinner on Friday?'));
  });

  test('replying to a photo or a voice note quotes a plain label; a pending message cannot be replied to', () async {
    expect(ChatController.excerpt(ChatMessage(id: '1', fromMe: false, kind: MsgKind.image, createdAt: DateTime.now())), 'Photo');
    expect(ChatController.excerpt(ChatMessage(id: '1', fromMe: false, kind: MsgKind.voice, createdAt: DateTime.now())), 'Voice note');
    final long = ChatMessage(id: '1', fromMe: false, kind: MsgKind.text, text: 'x' * 200, createdAt: DateTime.now());
    expect(ChatController.excerpt(long).length, lessThanOrEqualTo(81));
    final rig = await Rig.create();
    addTearDown(rig.container.dispose);
    await rig.load();
    final pending = ChatMessage(id: 'tmp-1', fromMe: true, kind: MsgKind.text, text: 'hi', createdAt: DateTime.now());
    await rig.ctrl.sendText('re', replyTo: pending);
    final p = MessagePayload.decode(await rig.bob.decryptDirect(conv, Uint8List.fromList(base64.decode(rig.server.stored.last.ciphertext))))!;
    expect(p.replyTo, isNull, reason: 'an unsent message has no id to point at');
  });

  test('reactions are control messages: never bubbles, one per person, toggled by repeating', () async {
    final rig = await Rig.create();
    addTearDown(rig.container.dispose);
    rig.server.fromBob(await rig.bobSays('I got the job!'));
    await rig.load();
    final theirs = rig.state.messages.single;

    await rig.ctrl.react(theirs, '🎉');
    expect(shown(rig).length, 1, reason: 'a reaction is not a bubble');
    expect(shown(rig).single.reactions, {'me': '🎉'});
    final sent = MessagePayload.decode(await rig.bob.decryptDirect(conv, Uint8List.fromList(base64.decode(rig.server.stored.last.ciphertext))))!;
    expect((sent.ctl, sent.target, sent.emoji), ('react', theirs.id, '🎉'));

    await rig.ctrl.react(theirs, '❤️'); // changing reaction replaces mine
    expect(shown(rig).single.reactions, {'me': '❤️'});
    await rig.ctrl.react(theirs, '❤️'); // same again takes it back
    expect(shown(rig).single.reactions, isEmpty);

    // Their reaction to my message, arriving over the wire.
    await rig.ctrl.sendText('Congratulations!');
    final mine = shown(rig).last;
    rig.server.fromBob(await rig.bobSendsPayload(MessagePayload.react(mine.id, '🙏')));
    await rig.ctrl.refresh();
    expect(shown(rig).last.reactions, {'them': '🙏'});
    expect(shown(rig).length, 2);
  });

  test('unsend deletes my message on the server, here, and tells the other app; I cannot unsend theirs', () async {
    final rig = await Rig.create();
    addTearDown(rig.container.dispose);
    rig.server.fromBob(await rig.bobSays('hello'));
    await rig.load();
    await rig.ctrl.sendText('oops, wrong chat');
    final mine = shown(rig).last;

    await rig.ctrl.unsend(rig.state.messages.first); // theirs: nothing happens
    expect(rig.server.unsent, isEmpty);

    await rig.ctrl.unsend(mine);
    expect(rig.server.unsent, [mine.id]);
    expect(shown(rig).map((m) => m.text), ['hello']);
    expect((await rig.cache.load(conv)).containsKey(mine.id), isFalse, reason: 'also gone from the local cache');
    final notice = MessagePayload.decode(await rig.bob.decryptDirect(conv, Uint8List.fromList(base64.decode(rig.server.stored.last.ciphertext))))!;
    expect((notice.ctl, notice.target), ('del', mine.id));
  });

  test('an unsend notice from the other person hides their message; one aimed at my message is ignored', () async {
    final rig = await Rig.create();
    addTearDown(rig.container.dispose);
    rig.server.fromBob(await rig.bobSays('this was a mistake'));
    await rig.load();
    await rig.ctrl.sendText('my own words');
    final theirs = rig.state.messages.firstWhere((m) => !m.fromMe);
    final mine = rig.state.messages.firstWhere((m) => m.fromMe);

    rig.server.fromBob(await rig.bobSendsPayload(MessagePayload.unsend(theirs.id)));
    rig.server.fromBob(await rig.bobSendsPayload(MessagePayload.unsend(mine.id))); // forged: not his message
    await rig.ctrl.refresh();
    expect(shown(rig).map((m) => m.text), ['my own words']);
  });
}
