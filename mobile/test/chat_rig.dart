import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:connectme/features/chat/chat_controller.dart';
import 'package:connectme/features/chat/chat_providers.dart';
import 'package:connectme/features/chat/crypto/crypto_service.dart';
import 'package:connectme/features/chat/crypto/persistent_signal_store.dart';
import 'package:connectme/features/chat/crypto/secret_store.dart';
import 'package:connectme/features/chat/data/chat_models.dart';
import 'package:connectme/features/chat/data/chat_repository.dart';
import 'package:connectme/features/chat/data/chat_security.dart';
import 'package:connectme/features/chat/data/message_cache.dart';
import 'package:connectme/features/chat/data/realtime_client.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'helpers.dart';

const conv = 'match-1';

/// A stand-in for the server: stores ciphertext blobs only.
class FakeServer extends ChatRepository {
  FakeServer(this.bobBundle) : super(fakeClient((o) async => Reply(404)));

  final Future<Map<String, dynamic>> Function() bobBundle;
  final List<WireMessage> stored = [];
  final Map<String, Uint8List> blobs = {};
  Map<String, dynamic>? uploadedKeys;
  int sends = 0;
  int _n = 0;

  @override
  Future<void> uploadKeys(Map<String, dynamic> payload) async => uploadedKeys = payload;
  @override
  Future<int> preKeyCount() async => 100;
  @override
  Future<Map<String, dynamic>> peerBundle(String matchId) => bobBundle();
  @override
  Future<List<WireMessage>> messages(String convId, {String? before}) async => List.of(stored);
  @override
  Future<void> markRead(String convId) async {}
  @override
  Future<void> deleteConversation(String convId) async => stored.clear();

  @override
  Future<WireMessage> send(String convId, {required String type, required String ciphertextB64, String? mediaId, int burnAfterS = 0}) async {
    sends++;
    final w = _wire('s${(++_n).toString().padLeft(3, '0')}', type, ciphertextB64, fromMe: true, mediaId: mediaId, burn: burnAfterS);
    stored.add(w);
    return w;
  }

  @override
  Future<String> uploadMedia(String convId, Uint8List encrypted) async {
    final id = 'media-${blobs.length + 1}';
    blobs[id] = encrypted;
    return id;
  }

  @override
  Future<Uint8List> downloadMedia(String mediaId) async => blobs[mediaId]!;

  WireMessage _wire(String id, String type, String ct, {required bool fromMe, String? mediaId, int burn = 0}) => WireMessage(
        id: id,
        type: type,
        ciphertext: ct,
        fromMe: fromMe,
        senderToken: fromMe ? 'tokMe' : 'tokBob',
        createdAt: DateTime.now(),
        mediaId: mediaId,
        burnAfterS: burn,
      );

  /// Delivers a message from Bob as the server would store it.
  WireMessage fromBob(String ciphertextB64, {String? mediaId}) {
    final w = _wire('b${(++_n).toString().padLeft(3, '0')}', 'text', ciphertextB64, fromMe: false, mediaId: mediaId);
    stored.add(w);
    return w;
  }
}

class FakeRealtime implements RealtimeClient {
  final msg = StreamController<Map<String, dynamic>>.broadcast();
  final typ = StreamController<Map<String, dynamic>>.broadcast();
  final rec = StreamController<Map<String, dynamic>>.broadcast();
  final cls = StreamController<String>.broadcast();
  @override
  Stream<Map<String, dynamic>> get messages => msg.stream;
  @override
  Stream<Map<String, dynamic>> get typing => typ.stream;
  @override
  Stream<Map<String, dynamic>> get receipts => rec.stream;
  @override
  Stream<String> get closed => cls.stream;
  @override
  bool get connected => false; // forces the REST path for sends
  @override
  Future<void> connect() async {}
  @override
  Future<bool> join(String matchId) async => true;
  @override
  void leave(String matchId) {}
  @override
  Future<Map<String, dynamic>?> sendMessage(String matchId, Map<String, dynamic> body) async => null;
  @override
  void setTyping(String matchId, bool typing) {}
  @override
  Future<bool> readReceipt(String matchId) async => false;
  @override
  void dispose() {}
}

Map<String, dynamic> wireJson(WireMessage w) => {
      'id': w.id,
      'match_id': conv,
      'type': w.type,
      'ciphertext': w.ciphertext,
      'from_me': w.fromMe,
      'sender_token': w.senderToken,
      'created_at': w.createdAt.toIso8601String(),
      'burn_after_s': w.burnAfterS,
    };

class Rig {
  Rig._(this.container, this.server, this.rt, this.bob, this.aliceCrypto, this.cache);
  final ProviderContainer container;
  final FakeServer server;
  final FakeRealtime rt;
  final CryptoService bob;
  final CryptoService aliceCrypto;
  final MessageCache cache;

  static Future<Rig> create({MemorySecretStore? aliceSecrets}) async {
    final secrets = aliceSecrets ?? MemorySecretStore();
    final alice = CryptoService(PersistentSignalStore(secrets, 'alice'));
    final bob = CryptoService(PersistentSignalStore(MemorySecretStore(), 'bob'));
    await bob.ensureKeys();
    await alice.ensureKeys();

    Future<Map<String, dynamic>> bobBundle() async {
      final up = await bob.uploadPayload();
      return {
        'registration_id': up['registration_id'],
        'identity_key': up['identity_key'],
        'signed_prekey_id': up['signed_prekey_id'],
        'signed_prekey': up['signed_prekey'],
        'signed_prekey_sig': up['signed_prekey_sig'],
        'prekey': (up['one_time_prekeys'] as List).first,
      };
    }

    final server = FakeServer(bobBundle);
    final rt = FakeRealtime();
    final cache = MessageCache(secrets, 'alice');
    final container = ProviderContainer(overrides: [
      chatRepositoryProvider.overrideWithValue(server),
      chatSecurityProvider.overrideWithValue(ChatSecurity(crypto: alice, repo: server, secrets: secrets, ns: 'alice')),
      messageCacheProvider.overrideWithValue(cache),
      realtimeProvider.overrideWithValue(rt),
    ]);
    // autoDispose providers die between reads unless something listens.
    container.listen(chatControllerProvider((id: conv, group: false)), (_, _) {});
    return Rig._(container, server, rt, bob, alice, cache);
  }

  /// Bob -> Alice (Alice's bundle is what the server would hand Bob). Built
  /// straight from Alice's store: her one-time keys are already marked as
  /// uploaded by the time a test calls this.
  Future<String> bobSays(String text) async {
    final store = aliceCrypto.store;
    final identity = await store.getIdentityKeyPair();
    final spk = (await store.loadSignedPreKeys()).first;
    final preId = (await store.preKeyIds()).first;
    final pre = await store.loadPreKey(preId);
    final bundle = {
      'registration_id': await store.getLocalRegistrationId(),
      'identity_key': base64.encode(identity.getPublicKey().serialize()),
      'signed_prekey_id': spk.id,
      'signed_prekey': base64.encode(spk.getKeyPair().publicKey.serialize()),
      'signed_prekey_sig': base64.encode(spk.signature),
      'prekey': {'key_id': preId, 'public_key': base64.encode(pre.getKeyPair().publicKey.serialize())},
    };
    final payload = MessagePayload.text(text).encode();
    final bytes = await bob.encryptDirect(conv, Uint8List.fromList(payload), () async => bundle);
    return base64.encode(bytes);
  }

  ChatController get ctrl => container.read(chatControllerProvider((id: conv, group: false)).notifier);
  Future<ChatState> load() => container.read(chatControllerProvider((id: conv, group: false)).future);
  ChatState get state => container.read(chatControllerProvider((id: conv, group: false))).requireValue;
}

