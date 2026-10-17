import 'dart:convert';
import 'dart:typed_data';

import 'package:connectme/core/network/api_client.dart';
import 'package:connectme/features/chat/chat_controller.dart';
import 'package:connectme/features/chat/crypto/secret_store.dart';
import 'package:connectme/features/chat/data/chat_models.dart';
import 'package:connectme/features/chat/data/chat_repository.dart';
import 'package:connectme/features/chat/data/chat_security.dart';
import 'package:connectme/features/chat/chat_providers.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'chat_rig.dart';

void main() {
  test('history is decrypted once, then served from the local cache', () async {
    final rig = await Rig.create();
    addTearDown(rig.container.dispose);
    rig.server.fromBob(await rig.bobSays('hello alice'));
    rig.server.fromBob(await rig.bobSays('are you free tonight?'));

    var s = await rig.load();
    expect(s.messages.map((m) => m.text), ['hello alice', 'are you free tonight?']);
    expect(s.messages.every((m) => !m.fromMe && m.kind == MsgKind.text), isTrue);

    // A Signal message key can be used only once: reloading must NOT try to
    // decrypt again, it must come from the cache.
    rig.container.invalidate(chatControllerProvider((id: conv, group: false)));
    s = await rig.load();
    expect(s.messages.map((m) => m.text), ['hello alice', 'are you free tonight?'],
        reason: 'second load must use the cached plaintext');
  });

  test('sent messages are encrypted for the peer and remembered locally', () async {
    final rig = await Rig.create();
    addTearDown(rig.container.dispose);
    await rig.load();

    await rig.ctrl.sendText('  meet at 8 — secret plan  ');
    expect(rig.server.sends, 1);
    final stored = rig.server.stored.single;
    expect(stored.ciphertext.contains('secret'), isFalse);
    expect(utf8.decode(base64.decode(stored.ciphertext), allowMalformed: true).contains('secret plan'), isFalse);

    // Bob can read it; the server could not.
    final plain = await rig.bob.decryptDirect(conv, Uint8List.fromList(base64.decode(stored.ciphertext)));
    expect(MessagePayload.decode(plain)!.text, 'meet at 8 — secret plan');

    expect(rig.state.messages.single.pending, isFalse);
    expect(rig.state.messages.single.fromMe, isTrue);

    // After a reload the server only has ciphertext meant for Bob; our own
    // plaintext must come back from the cache.
    rig.container.invalidate(chatControllerProvider((id: conv, group: false)));
    final s = await rig.load();
    expect(s.messages.single.text, 'meet at 8 — secret plan');
  });

  test('a realtime message is decrypted and appended; echoes are ignored', () async {
    final rig = await Rig.create();
    addTearDown(rig.container.dispose);
    await rig.load();

    final w = rig.server.fromBob(await rig.bobSays('live message'));
    rig.rt.msg.add(wireJson(w));
    await Future<void>.delayed(const Duration(milliseconds: 50));
    expect(rig.state.messages.map((m) => m.text), ['live message']);

    rig.rt.msg.add(wireJson(w)); // duplicate delivery
    await Future<void>.delayed(const Duration(milliseconds: 50));
    expect(rig.state.messages.length, 1);

    rig.rt.msg.add({...wireJson(w), 'match_id': 'other-conv', 'id': 'zzz'});
    await Future<void>.delayed(const Duration(milliseconds: 50));
    expect(rig.state.messages.length, 1, reason: 'messages for other conversations are ignored');
  });

  test('typing and read receipts update the state', () async {
    final rig = await Rig.create();
    addTearDown(rig.container.dispose);
    await rig.load();
    await rig.ctrl.sendText('ping');

    rig.rt.typ.add({'match_id': conv, 'is_typing': true});
    await Future<void>.delayed(const Duration(milliseconds: 30));
    expect(rig.state.peerTyping, isTrue);
    rig.rt.typ.add({'match_id': conv, 'is_typing': false});
    await Future<void>.delayed(const Duration(milliseconds: 30));
    expect(rig.state.peerTyping, isFalse);

    expect(rig.state.messages.single.readAt, isNull);
    rig.rt.rec.add({'match_id': conv, 'count': 1});
    await Future<void>.delayed(const Duration(milliseconds: 30));
    expect(rig.state.messages.single.readAt, isNotNull);
  });

  test('garbage ciphertext becomes a locked placeholder, not a crash or a cache entry', () async {
    final rig = await Rig.create();
    addTearDown(rig.container.dispose);
    rig.server.fromBob(base64.encode([3, 1, 2, 3, 4]));
    final s = await rig.load();
    expect(s.messages.single.kind, MsgKind.locked);
    expect((await rig.cache.load(conv)), isEmpty, reason: 'failures are not cached so they can be retried');
  });

  test('image attachments are encrypted before upload and decrypt for the peer', () async {
    final rig = await Rig.create();
    addTearDown(rig.container.dispose);
    await rig.load();

    final photo = Uint8List.fromList(List.generate(2000, (i) => (i * 7) % 251));
    await rig.ctrl.sendImage(photo);

    final blob = rig.server.blobs.values.single;
    expect(blob.length, greaterThan(photo.length));
    expect(_contains(blob, photo.sublist(0, 64)), isFalse, reason: 'plaintext must not be in the uploaded blob');

    // Bob decrypts the message to get the attachment key, then the blob.
    final msg = rig.server.stored.single;
    expect(msg.type, 'image');
    final plain = await rig.bob.decryptDirect(conv, Uint8List.fromList(base64.decode(msg.ciphertext)));
    final p = MessagePayload.decode(plain)!;
    expect(p.type, 'image');
    final opened = await ChatSecurity.decryptAttachment(blob, p.key!);
    expect(opened, photo);

    // And the sender can open her own copy.
    expect(await rig.ctrl.openAttachment(rig.state.messages.single), photo);
  });

  test('burn timers hide messages only after they were read', () {
    final now = DateTime(2026, 1, 1, 12);
    ChatMessage m({DateTime? readAt}) => ChatMessage(
        id: 'x', fromMe: false, kind: MsgKind.text, createdAt: now, text: 't', burnAfterS: 30, readAt: readAt);
    final unread = ChatState(messages: [m()]);
    final fresh = ChatState(messages: [m(readAt: now.subtract(const Duration(seconds: 10)))]);
    final burnt = ChatState(messages: [m(readAt: now.subtract(const Duration(seconds: 31)))]);
    expect(unread.visible(now), hasLength(1));
    expect(fresh.visible(now), hasLength(1));
    expect(burnt.visible(now), isEmpty);
  });

  test('a failed send removes the optimistic message and surfaces the error', () async {
    final rig = await Rig.create();
    addTearDown(rig.container.dispose);
    await rig.load();
    final failing = FailingServer(rig.server);
    final c2 = ProviderContainer(overrides: [
      chatRepositoryProvider.overrideWithValue(failing),
      chatSecurityProvider.overrideWithValue(ChatSecurity(crypto: rig.aliceCrypto, repo: failing, secrets: MemorySecretStore(), ns: 'alice')),
      messageCacheProvider.overrideWithValue(rig.cache),
      realtimeProvider.overrideWithValue(rig.rt),
    ]);
    addTearDown(c2.dispose);
    c2.listen(chatControllerProvider((id: conv, group: false)), (_, _) {});
    await c2.read(chatControllerProvider((id: conv, group: false)).future);
    await expectLater(
      c2.read(chatControllerProvider((id: conv, group: false)).notifier).sendText('hi'),
      throwsA(isA<ApiException>()),
    );
    expect(c2.read(chatControllerProvider((id: conv, group: false))).requireValue.messages, isEmpty);
  });
}

class FailingServer extends FakeServer {
  FailingServer(FakeServer base) : super(base.bobBundle);
  @override
  Future<WireMessage> send(String convId, {required String type, required String ciphertextB64, String? mediaId, int burnAfterS = 0}) =>
      throw ApiException(429, 'rate limit exceeded');
}

bool _contains(Uint8List hay, List<int> needle) {
  outer:
  for (var i = 0; i <= hay.length - needle.length; i++) {
    for (var j = 0; j < needle.length; j++) {
      if (hay[i + j] != needle[j]) continue outer;
    }
    return true;
  }
  return false;
}
