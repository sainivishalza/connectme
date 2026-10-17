import 'dart:convert';
import 'dart:typed_data';

import 'package:connectme/features/chat/crypto/crypto_service.dart';
import 'package:connectme/features/chat/crypto/persistent_signal_store.dart';
import 'package:connectme/features/chat/crypto/secret_store.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:libsignal_protocol_dart/libsignal_protocol_dart.dart';

Uint8List utf(String s) => Uint8List.fromList(utf8.encode(s));
String str(Uint8List b) => utf8.decode(b);

class Peer {
  Peer(this.secrets, String ns) : crypto = CryptoService(PersistentSignalStore(secrets, ns));
  final MemorySecretStore secrets;
  final CryptoService crypto;

  static Future<Peer> create(String ns, [MemorySecretStore? s]) async {
    final p = Peer(s ?? MemorySecretStore(), ns);
    await p.crypto.ensureKeys();
    return p;
  }

  /// What the key server would return to someone fetching this peer's bundle:
  /// the public bundle plus ONE consumed one-time prekey.
  Future<Map<String, dynamic>> bundleFor(int i) async {
    final up = await crypto.uploadPayload();
    final pre = (up['one_time_prekeys'] as List)[i] as Map;
    return {
      'registration_id': up['registration_id'],
      'identity_key': up['identity_key'],
      'signed_prekey_id': up['signed_prekey_id'],
      'signed_prekey': up['signed_prekey'],
      'signed_prekey_sig': up['signed_prekey_sig'],
      'prekey': pre,
    };
  }
}

void main() {
  group('upload payload matches what the server validates', () {
    test('key sizes, ids and counts', () async {
      final p = await Peer.create('u1');
      final up = await p.crypto.uploadPayload();
      int len(String k) => base64.decode(up[k] as String).length;
      expect(len('identity_key'), inInclusiveRange(32, 128));
      expect(len('signed_prekey'), inInclusiveRange(32, 128));
      expect(len('signed_prekey_sig'), inInclusiveRange(32, 128));
      expect(up['registration_id'] as int, greaterThan(0));
      final pre = up['one_time_prekeys'] as List;
      expect(pre.length, 100); // server accepts at most 100 per upload
      final ids = pre.map((e) => (e as Map)['key_id'] as int).toSet();
      expect(ids.length, 100);
      for (final e in pre) {
        expect(base64.decode((e as Map)['public_key'] as String).length, inInclusiveRange(32, 128));
      }
    });

    test('replenish only when low and never reuses ids', () async {
      final p = await Peer.create('u1');
      final first = await p.crypto.uploadPayload();
      final firstIds = (first['one_time_prekeys'] as List).map((e) => (e as Map)['key_id']).toSet();
      await p.crypto.markUploaded(first);
      expect(await p.crypto.replenishIfNeeded(80), isNull);

      final more = await p.crypto.replenishIfNeeded(5); // server has 5 left
      expect(more, isNotNull);
      final fresh = (more!['one_time_prekeys'] as List).map((e) => (e as Map)['key_id']).toList();
      expect(fresh.length, 95, reason: 'tops the server back up to 100');
      expect(fresh.where(firstIds.contains), isEmpty, reason: 'only new keys are sent, ids never reused');

      // Nothing new to send once uploaded.
      await p.crypto.markUploaded(more);
      expect((await p.crypto.uploadPayload())['one_time_prekeys'], isEmpty);
    });
  });

  group('1:1 conversations', () {
    test('round trip, replies, many messages, no plaintext on the wire', () async {
      final alice = await Peer.create('alice');
      final bob = await Peer.create('bob');
      const conv = 'match-1';

      final first = await alice.crypto.encryptDirect(conv, utf('hello bob, secret plan'), () => bob.bundleFor(0));
      expect(first[0], CiphertextMessage.prekeyType);
      expect(utf8.decode(first, allowMalformed: true).contains('secret plan'), isFalse);
      expect(str(await bob.crypto.decryptDirect(conv, first)), 'hello bob, secret plan');

      final reply = await bob.crypto.encryptDirect(conv, utf('hi alice'), () => throw StateError('session exists'));
      expect(reply[0], CiphertextMessage.whisperType);
      expect(str(await alice.crypto.decryptDirect(conv, reply)), 'hi alice');

      for (var i = 0; i < 25; i++) {
        final a = await alice.crypto.encryptDirect(conv, utf('a$i'), () => throw StateError('no fetch'));
        expect(str(await bob.crypto.decryptDirect(conv, a)), 'a$i');
        final b = await bob.crypto.encryptDirect(conv, utf('b$i'), () => throw StateError('no fetch'));
        expect(str(await alice.crypto.decryptDirect(conv, b)), 'b$i');
      }
    });

    test('out-of-order delivery still decrypts', () async {
      final alice = await Peer.create('alice');
      final bob = await Peer.create('bob');
      final m0 = await alice.crypto.encryptDirect('c', utf('0'), () => bob.bundleFor(1));
      await bob.crypto.decryptDirect('c', m0);
      final m1 = await alice.crypto.encryptDirect('c', utf('1'), () => throw StateError('x'));
      final m2 = await alice.crypto.encryptDirect('c', utf('2'), () => throw StateError('x'));
      expect(str(await bob.crypto.decryptDirect('c', m2)), '2');
      expect(str(await bob.crypto.decryptDirect('c', m1)), '1');
    });

    test('tampered ciphertext is rejected', () async {
      final alice = await Peer.create('alice');
      final bob = await Peer.create('bob');
      final m = await alice.crypto.encryptDirect('c', utf('top secret'), () => bob.bundleFor(0));
      final bad = Uint8List.fromList(m)..[m.length - 5] ^= 0xFF;
      await expectLater(bob.crypto.decryptDirect('c', bad), throwsA(anything));
    });

    test('a third party cannot read a conversation', () async {
      final alice = await Peer.create('alice');
      final bob = await Peer.create('bob');
      final eve = await Peer.create('eve');
      final m = await alice.crypto.encryptDirect('c', utf('private'), () => bob.bundleFor(0));
      await expectLater(eve.crypto.decryptDirect('c', m), throwsA(anything));
    });

    test('sessions survive an app restart (state is persisted)', () async {
      final aliceSecrets = MemorySecretStore();
      final bobSecrets = MemorySecretStore();
      var alice = await Peer.create('alice', aliceSecrets);
      var bob = await Peer.create('bob', bobSecrets);
      final m1 = await alice.crypto.encryptDirect('c', utf('before restart'), () => bob.bundleFor(0));
      expect(str(await bob.crypto.decryptDirect('c', m1)), 'before restart');

      // "Restart": brand new service objects over the same secure storage.
      alice = Peer(aliceSecrets, 'alice');
      bob = Peer(bobSecrets, 'bob');
      final m2 = await alice.crypto.encryptDirect('c', utf('after restart'), () => throw StateError('no refetch'));
      expect(str(await bob.crypto.decryptDirect('c', m2)), 'after restart');
      final r = await bob.crypto.encryptDirect('c', utf('and back'), () => throw StateError('no refetch'));
      expect(str(await alice.crypto.decryptDirect('c', r)), 'and back');
    });

    test('a changed identity key is not silently trusted', () async {
      final alice = await Peer.create('alice');
      final bob = await Peer.create('bob');
      final m = await alice.crypto.encryptDirect('c', utf('hi'), () => bob.bundleFor(0));
      await bob.crypto.decryptDirect('c', m);
      final r = await bob.crypto.encryptDirect('c', utf('yo'), () => throw StateError('x'));
      await alice.crypto.decryptDirect('c', r);

      // An impostor (or reinstalled app) opens a new session claiming to be bob.
      final impostor = await Peer.create('bob2');
      final fresh = await alice.crypto.encryptDirect('other', utf('x'), () => impostor.bundleFor(0));
      expect(fresh, isNotEmpty);
      final fake = await impostor.crypto.encryptDirect('c', utf('i am bob'), () => alice.bundleFor(0));
      await expectLater(alice.crypto.decryptDirect('c', fake), throwsA(isA<UntrustedIdentityException>()));
    });

    test('both sides see the same safety number, and it changes if a key is swapped', () async {
      final alice = await Peer.create('alice');
      final bob = await Peer.create('bob');
      expect(await alice.crypto.safetyNumber('c'), isNull, reason: 'no session yet');
      final m = await alice.crypto.encryptDirect('c', utf('hi'), () => bob.bundleFor(0));
      await bob.crypto.decryptDirect('c', m);
      final a = await alice.crypto.safetyNumber('c');
      final b = await bob.crypto.safetyNumber('c');
      expect(a, isNotNull);
      expect(a, b, reason: 'comparing the two screens is the whole point');
      expect(RegExp(r'^(\d{5} ){11}\d{5}$').hasMatch(a!), isTrue, reason: '60 digits in groups of five');

      // A different third party gives a different number: a swapped key is visible.
      final carol = await Peer.create('carol');
      final m2 = await alice.crypto.encryptDirect('d', utf('hi'), () => carol.bundleFor(0));
      await carol.crypto.decryptDirect('d', m2);
      expect(await alice.crypto.safetyNumber('d'), isNot(a));
    });

    test('a changed key is refused until the person trusts it, then a new session works', () async {
      final alice = await Peer.create('alice');
      final bob = await Peer.create('bob');
      final m = await alice.crypto.encryptDirect('c', utf('hi'), () => bob.bundleFor(0));
      await bob.crypto.decryptDirect('c', m);
      final before = await alice.crypto.safetyNumber('c');

      // Bob reinstalls: same conversation, brand-new identity.
      final bob2 = await Peer.create('bob-reinstalled');
      final hello = await bob2.crypto.encryptDirect('c', utf('it is me again'), () => alice.bundleFor(1));
      await expectLater(alice.crypto.decryptDirect('c', hello), throwsA(isA<UntrustedIdentityException>()));
      expect(await alice.crypto.safetyNumber('c'), before, reason: 'the trusted key is unchanged until reviewed');

      await alice.crypto.trustNewIdentity('c');
      final hello2 = await bob2.crypto.encryptDirect('c', utf('hello again'), () => alice.bundleFor(2));
      expect(str(await alice.crypto.decryptDirect('c', hello2)), 'hello again');
      expect(await alice.crypto.safetyNumber('c'), isNot(before), reason: 'the number now reflects the new key');
    });

    test('key material is namespaced per user', () async {
      final shared = MemorySecretStore();
      final a = await Peer.create('userA', shared);
      final b = await Peer.create('userB', shared);
      final ia = (await a.crypto.uploadPayload())['identity_key'];
      final ib = (await b.crypto.uploadPayload())['identity_key'];
      expect(ia, isNot(ib));
    });
  });

  group('group conversations (sender keys)', () {
    test('three members: distribute keys pairwise, then group messages decrypt', () async {
      final a = await Peer.create('a'), b = await Peer.create('b'), c = await Peer.create('c');
      const group = 'gm-1';
      // Conversation-scoped member tokens, as the server provides them.
      const tA = 'tokA', tB = 'tokB', tC = 'tokC';

      // Each member sends their sender key to the other two through pairwise sessions.
      Future<void> distribute(Peer from, String fromToken, Map<String, Peer> to) async {
        final dist = await from.crypto.myDistribution(group);
        for (final e in to.entries) {
          // Pairwise session names are the *other* party's token on each side.
          final env = await from.crypto.encryptDirect(e.key, dist, () => e.value.bundleFor(0));
          final got = await e.value.crypto.decryptDirect(fromToken, env);
          await e.value.crypto.acceptDistribution(group, fromToken, got);
        }
      }

      await distribute(a, tA, {tB: b, tC: c});
      await distribute(b, tB, {tA: a, tC: c});
      await distribute(c, tC, {tA: a, tB: b});

      final m = await a.crypto.encryptGroup(group, utf('hello crew'));
      expect(m[0], CiphertextMessage.senderKeyType);
      expect(str(await b.crypto.decryptGroup(group, tA, m)), 'hello crew');
      expect(str(await c.crypto.decryptGroup(group, tA, m)), 'hello crew');

      final r = await c.crypto.encryptGroup(group, utf('hi from c'));
      expect(str(await a.crypto.decryptGroup(group, tC, r)), 'hi from c');
      expect(str(await b.crypto.decryptGroup(group, tC, r)), 'hi from c');
    });

    test('a non-member cannot decrypt, and wrong sender attribution fails', () async {
      final a = await Peer.create('a'), b = await Peer.create('b'), eve = await Peer.create('eve');
      const group = 'gm-2';
      final dist = await a.crypto.myDistribution(group);
      final env = await a.crypto.encryptDirect('tokB', dist, () => b.bundleFor(0));
      await b.crypto.acceptDistribution(group, 'tokA', await b.crypto.decryptDirect('tokA', env));

      final m = await a.crypto.encryptGroup(group, utf('members only'));
      await expectLater(eve.crypto.decryptGroup(group, 'tokA', m), throwsA(anything));
      await expectLater(b.crypto.decryptGroup(group, 'someoneElse', m), throwsA(anything));
      expect(str(await b.crypto.decryptGroup(group, 'tokA', m)), 'members only');
    });
  });
}
