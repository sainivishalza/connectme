import 'dart:convert';
import 'dart:typed_data';

import 'package:libsignal_protocol_dart/libsignal_protocol_dart.dart';

import 'secret_store.dart';

String _b64(List<int> b) => base64.encode(b);
Uint8List _unb64(String s) => Uint8List.fromList(base64.decode(s));

/// A complete Signal protocol store (identity, sessions, pre-keys, signed
/// pre-keys, sender keys) persisted to a [SecretStore]. Everything is
/// namespaced by [ns] (the user id) so two accounts on one device never share
/// key material.
class PersistentSignalStore extends SignalProtocolStore
    implements SenderKeyStore {
  PersistentSignalStore(this._s, this.ns);

  final SecretStore _s;
  final String ns;

  String _k(String part) => 'signal.$ns.$part';

  // --- identity --------------------------------------------------------

  /// Creates the identity key pair and registration id on first use.
  Future<void> ensureIdentity() async {
    if (await _s.read(_k('identity')) != null) return;
    await _s.write(_k('identity'), _b64(generateIdentityKeyPair().serialize()));
    await _s.write(_k('regid'), generateRegistrationId(false).toString());
  }

  @override
  Future<IdentityKeyPair> getIdentityKeyPair() async {
    final v = await _s.read(_k('identity'));
    if (v == null) throw StateError('identity not initialised');
    return IdentityKeyPair.fromSerialized(_unb64(v));
  }

  @override
  Future<int> getLocalRegistrationId() async =>
      int.parse((await _s.read(_k('regid')))!);

  @override
  Future<IdentityKey?> getIdentity(SignalProtocolAddress address) async {
    final v = await _s.read(_k('trust.$address'));
    return v == null ? null : IdentityKey.fromBytes(_unb64(v), 0);
  }

  @override
  Future<bool> saveIdentity(
    SignalProtocolAddress address,
    IdentityKey? identityKey,
  ) async {
    if (identityKey == null) return false;
    final existing = await _s.read(_k('trust.$address'));
    final now = _b64(identityKey.serialize());
    await _s.write(_k('trust.$address'), now);
    return existing != null && existing != now; // true = the key changed
  }

  /// Trust on first use. A peer whose identity key changes is NOT trusted
  /// again until [forgetIdentity] is called (after the user is warned).
  @override
  Future<bool> isTrustedIdentity(
    SignalProtocolAddress address,
    IdentityKey? identityKey,
    Direction? direction,
  ) async {
    if (identityKey == null) return false;
    final existing = await _s.read(_k('trust.$address'));
    return existing == null || existing == _b64(identityKey.serialize());
  }

  Future<void> forgetIdentity(SignalProtocolAddress address) async {
    await _s.delete(_k('trust.$address'));
    await deleteSession(address);
  }

  // --- helpers for indexed collections --------------------------------

  Future<List<String>> _index(String name) async {
    final v = await _s.read(_k('idx.$name'));
    return v == null ? [] : (jsonDecode(v) as List).cast<String>();
  }

  Future<void> _addIndex(String name, String entry) async {
    final idx = await _index(name);
    if (!idx.contains(entry)) {
      await _s.write(_k('idx.$name'), jsonEncode([...idx, entry]));
    }
  }

  Future<void> _removeIndex(String name, String entry) async {
    final idx = await _index(name);
    if (idx.remove(entry)) await _s.write(_k('idx.$name'), jsonEncode(idx));
  }

  // --- sessions --------------------------------------------------------

  @override
  Future<bool> containsSession(SignalProtocolAddress address) async =>
      await _s.read(_k('session.$address')) != null;

  @override
  Future<SessionRecord> loadSession(SignalProtocolAddress address) async {
    final v = await _s.read(_k('session.$address'));
    return v == null
        ? SessionRecord()
        : SessionRecord.fromSerialized(_unb64(v));
  }

  @override
  Future<void> storeSession(
    SignalProtocolAddress address,
    SessionRecord record,
  ) async {
    await _s.write(_k('session.$address'), _b64(record.serialize()));
    await _addIndex(
      'sessions',
      '${address.getName()}\u0000${address.getDeviceId()}',
    );
  }

  @override
  Future<void> deleteSession(SignalProtocolAddress address) async {
    await _s.delete(_k('session.$address'));
    await _removeIndex(
      'sessions',
      '${address.getName()}\u0000${address.getDeviceId()}',
    );
  }

  @override
  Future<void> deleteAllSessions(String name) async {
    for (final e in await _index('sessions')) {
      final parts = e.split('\u0000');
      if (parts[0] == name) {
        await deleteSession(SignalProtocolAddress(name, int.parse(parts[1])));
      }
    }
  }

  @override
  Future<List<int>> getSubDeviceSessions(String name) async => [
    for (final e in await _index('sessions'))
      if (e.split('\u0000')[0] == name && int.parse(e.split('\u0000')[1]) != 1)
        int.parse(e.split('\u0000')[1]),
  ];

  // --- one-time pre-keys ----------------------------------------------

  @override
  Future<PreKeyRecord> loadPreKey(int preKeyId) async {
    final v = await _s.read(_k('prekey.$preKeyId'));
    if (v == null) throw InvalidKeyIdException('No such prekey: $preKeyId');
    return PreKeyRecord.fromBuffer(_unb64(v));
  }

  @override
  Future<void> storePreKey(int preKeyId, PreKeyRecord record) async {
    await _s.write(_k('prekey.$preKeyId'), _b64(record.serialize()));
    await _addIndex('prekeys', '$preKeyId');
  }

  @override
  Future<bool> containsPreKey(int preKeyId) async =>
      await _s.read(_k('prekey.$preKeyId')) != null;

  @override
  Future<void> removePreKey(int preKeyId) async {
    await _s.delete(_k('prekey.$preKeyId'));
    await _removeIndex('prekeys', '$preKeyId');
  }

  Future<List<int>> preKeyIds() async => [
    for (final e in await _index('prekeys')) int.parse(e),
  ];

  /// Ids already uploaded to the server (so a top-up sends only new keys).
  Future<Set<int>> uploadedPreKeyIds() async => {
    for (final e in await _index('uploaded')) int.parse(e),
  };

  Future<void> markPreKeysUploaded(Iterable<int> ids) async {
    for (final id in ids) {
      await _addIndex('uploaded', '$id');
    }
  }

  /// Next unused one-time pre-key id (monotonic, never reused).
  Future<int> nextPreKeyId() async =>
      int.parse(await _s.read(_k('prekey.next')) ?? '1');

  Future<void> setNextPreKeyId(int id) => _s.write(_k('prekey.next'), '$id');

  // --- signed pre-keys -------------------------------------------------

  @override
  Future<SignedPreKeyRecord> loadSignedPreKey(int id) async {
    final v = await _s.read(_k('spk.$id'));
    if (v == null) throw InvalidKeyIdException('No such signed prekey: $id');
    return SignedPreKeyRecord.fromSerialized(_unb64(v));
  }

  @override
  Future<List<SignedPreKeyRecord>> loadSignedPreKeys() async => [
    for (final e in await _index('spks')) await loadSignedPreKey(int.parse(e)),
  ];

  @override
  Future<void> storeSignedPreKey(int id, SignedPreKeyRecord record) async {
    await _s.write(_k('spk.$id'), _b64(record.serialize()));
    await _addIndex('spks', '$id');
  }

  @override
  Future<bool> containsSignedPreKey(int id) async =>
      await _s.read(_k('spk.$id')) != null;

  @override
  Future<void> removeSignedPreKey(int id) async {
    await _s.delete(_k('spk.$id'));
    await _removeIndex('spks', '$id');
  }

  // --- sender keys (groups) --------------------------------------------

  @override
  Future<SenderKeyRecord> loadSenderKey(SenderKeyName name) async {
    final v = await _s.read(_k('sk.${name.serialize()}'));
    return v == null
        ? SenderKeyRecord()
        : SenderKeyRecord.fromSerialized(_unb64(v));
  }

  @override
  Future<void> storeSenderKey(SenderKeyName name, SenderKeyRecord record) =>
      _s.write(_k('sk.${name.serialize()}'), _b64(record.serialize()));
}
