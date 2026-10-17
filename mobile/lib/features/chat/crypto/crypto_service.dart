import 'dart:convert';
import 'dart:typed_data';

import 'package:cryptography/cryptography.dart' show Sha256;
import 'package:libsignal_protocol_dart/libsignal_protocol_dart.dart';

import 'persistent_signal_store.dart';

/// Server key bundle as returned by GET /chat/keys/{match} (one peer).
typedef BundleJson = Map<String, dynamic>;

/// Fetches a peer's public bundle for a conversation (consumes a prekey).
typedef BundleFetcher = Future<BundleJson> Function();

/// End-to-end encryption for chat. This class never touches the network: the
/// caller supplies bundles and carries ciphertext.
///
/// Wire format: 1 type byte (CiphertextMessage type) followed by the Signal
/// message bytes. The server only ever stores/relays this blob.
class CryptoService {
  CryptoService(this.store);

  final PersistentSignalStore store;

  static const _device = 1;
  static const _selfName = 'self';
  static const initialPreKeys = 100;
  static const lowWater = 20;

  // --- key lifecycle ---------------------------------------------------

  /// Creates identity, signed pre-key and the first batch of one-time keys.
  Future<void> ensureKeys() async {
    await store.ensureIdentity();
    if ((await store.loadSignedPreKeys()).isEmpty) {
      final id = await store.getIdentityKeyPair();
      await store.storeSignedPreKey(1, generateSignedPreKey(id, 1));
    }
    if ((await store.preKeyIds()).isEmpty) {
      await _generatePreKeys(initialPreKeys);
    }
  }

  Future<void> _generatePreKeys(int count) async {
    final start = await store.nextPreKeyId();
    for (final k in generatePreKeys(start, count)) {
      await store.storePreKey(k.id, k);
    }
    await store.setNextPreKeyId(start + count);
  }

  /// JSON for POST /chat/keys: the public bundle plus up to 100 one-time keys
  /// that have not been uploaded yet. Call [markUploaded] once the server
  /// accepted it.
  Future<Map<String, dynamic>> uploadPayload() async {
    final identity = await store.getIdentityKeyPair();
    final spk = (await store.loadSignedPreKeys()).first;
    final uploaded = await store.uploadedPreKeyIds();
    final pre = <Map<String, dynamic>>[];
    for (final id in await store.preKeyIds()) {
      if (uploaded.contains(id)) continue;
      final r = await store.loadPreKey(id);
      pre.add({
        'key_id': id,
        'public_key': base64.encode(r.getKeyPair().publicKey.serialize()),
      });
    }
    return {
      'registration_id': await store.getLocalRegistrationId(),
      'identity_key': base64.encode(identity.getPublicKey().serialize()),
      'signed_prekey_id': spk.id,
      'signed_prekey': base64.encode(spk.getKeyPair().publicKey.serialize()),
      'signed_prekey_sig': base64.encode(spk.signature),
      'one_time_prekeys': pre.take(100).toList(),
    };
  }

  /// Records that the one-time keys in [payload] are now on the server.
  Future<void> markUploaded(Map<String, dynamic> payload) =>
      store.markPreKeysUploaded([
        for (final e in (payload['one_time_prekeys'] as List))
          (e as Map)['key_id'] as int,
      ]);

  /// Generates more one-time keys when [remainingOnServer] runs low and
  /// returns an upload payload, or null if none are needed.
  Future<Map<String, dynamic>?> replenishIfNeeded(int remainingOnServer) async {
    if (remainingOnServer >= lowWater) return null;
    await _generatePreKeys(initialPreKeys - remainingOnServer);
    return uploadPayload();
  }

  // --- 1:1 -------------------------------------------------------------

  SignalProtocolAddress _addr(String name) =>
      SignalProtocolAddress(name, _device);

  /// Encrypts for the peer of conversation [peer]. If no session exists the
  /// session is built from [fetchBundle] (a prekey message results).
  Future<Uint8List> encryptDirect(
    String peer,
    Uint8List plaintext,
    BundleFetcher fetchBundle,
  ) async {
    final remote = _addr(peer);
    if (!await store.containsSession(remote)) {
      await SessionBuilder.fromSignalStore(
        store,
        remote,
      ).processPreKeyBundle(_toBundle(await fetchBundle()));
    }
    final msg = await SessionCipher.fromStore(store, remote).encrypt(plaintext);
    return _wrap(msg.getType(), msg.serialize());
  }

  /// A short fingerprint of one identity key: six groups of five digits.
  static Future<String> _fingerprint(Uint8List identity) async {
    final h = await Sha256().hash([...utf8.encode('connectme-safety-v1'), ...identity]);
    final b = h.bytes;
    final out = StringBuffer();
    for (var i = 0; i < 6; i++) {
      final v = ((b[i * 4] << 24) | (b[i * 4 + 1] << 16) | (b[i * 4 + 2] << 8) | b[i * 4 + 3]) % 100000;
      out.write(v.toString().padLeft(5, '0'));
    }
    return out.toString();
  }

  /// The safety number for the conversation with [peer]: both people's
  /// identity keys, combined in a fixed order so each side shows the same 60
  /// digits. If the two of you read the same number to each other (in person
  /// or over another channel), nobody in between has swapped a key. Null until
  /// a session exists.
  Future<String?> safetyNumber(String peer) async {
    final theirs = await store.getIdentity(_addr(peer));
    if (theirs == null) return null;
    final mine = (await store.getIdentityKeyPair()).getPublicKey().serialize();
    final parts = [await _fingerprint(mine), await _fingerprint(theirs.serialize())]..sort();
    final all = parts.join();
    return [for (var i = 0; i < all.length; i += 5) all.substring(i, i + 5)].join(' ');
  }

  /// Accepts a peer's new identity key after the person has reviewed it. The
  /// old session is dropped, so the next message starts a fresh one.
  Future<void> trustNewIdentity(String peer) => store.forgetIdentity(_addr(peer));

  Future<Uint8List> decryptDirect(String peer, Uint8List data) async {
    final remote = _addr(peer);
    final cipher = SessionCipher.fromStore(store, remote);
    final type = data[0];
    final body = Uint8List.sublistView(data, 1);
    switch (type) {
      case CiphertextMessage.prekeyType:
        return cipher.decrypt(PreKeySignalMessage(body));
      case CiphertextMessage.whisperType:
        return cipher.decryptFromSignal(SignalMessage.fromSerialized(body));
      default:
        throw FormatException('unexpected message type $type');
    }
  }

  PreKeyBundle _toBundle(BundleJson j) {
    final pk = j['prekey'] as Map?;
    return PreKeyBundle(
      j['registration_id'] as int,
      _device,
      pk == null ? null : pk['key_id'] as int,
      pk == null
          ? null
          : Curve.decodePoint(
              Uint8List.fromList(base64.decode(pk['public_key'] as String)),
              0,
            ),
      j['signed_prekey_id'] as int,
      Curve.decodePoint(
        Uint8List.fromList(base64.decode(j['signed_prekey'] as String)),
        0,
      ),
      Uint8List.fromList(base64.decode(j['signed_prekey_sig'] as String)),
      IdentityKey.fromBytes(
        Uint8List.fromList(base64.decode(j['identity_key'] as String)),
        0,
      ),
    );
  }

  // --- groups (sender keys) -------------------------------------------

  SenderKeyName _own(String group) => SenderKeyName(group, _addr(_selfName));
  SenderKeyName _of(String group, String senderToken) =>
      SenderKeyName(group, _addr(senderToken));

  /// My sender-key distribution message for [group]; send it to every member
  /// through their pairwise session ([encryptDirect]).
  Future<Uint8List> myDistribution(String group) async {
    final m = await GroupSessionBuilder(store).create(_own(group));
    return m.serialize();
  }

  Future<void> acceptDistribution(
    String group,
    String senderToken,
    Uint8List distribution,
  ) async {
    await GroupSessionBuilder(store).process(
      _of(group, senderToken),
      SenderKeyDistributionMessageWrapper.fromSerialized(distribution),
    );
  }

  Future<Uint8List> encryptGroup(String group, Uint8List plaintext) async {
    final c = await GroupCipher(store, _own(group)).encrypt(plaintext);
    return _wrap(CiphertextMessage.senderKeyType, c);
  }

  Future<Uint8List> decryptGroup(
    String group,
    String senderToken,
    Uint8List data,
  ) async {
    if (data.isEmpty || data[0] != CiphertextMessage.senderKeyType) {
      throw FormatException('not a group message');
    }
    return GroupCipher(
      store,
      _of(group, senderToken),
    ).decrypt(Uint8List.sublistView(data, 1));
  }

  Uint8List _wrap(int type, Uint8List body) =>
      Uint8List.fromList([type, ...body]);
}
