import 'package:flutter_secure_storage/flutter_secure_storage.dart';

class Tokens {
  const Tokens({required this.access, required this.refresh});
  final String access;
  final String refresh;
}

/// Where the session lives. Tokens go in the platform keystore (Keychain /
/// Keystore), never in plain preferences.
abstract class TokenStore {
  Future<Tokens?> read();
  Future<void> write(Tokens tokens);
  Future<void> clear();
}

class SecureTokenStore implements TokenStore {
  SecureTokenStore([FlutterSecureStorage? storage])
      : _s = storage ?? const FlutterSecureStorage();
  final FlutterSecureStorage _s;

  static const _a = 'access_token';
  static const _r = 'refresh_token';

  @override
  Future<Tokens?> read() async {
    final a = await _s.read(key: _a);
    final r = await _s.read(key: _r);
    return (a == null || r == null) ? null : Tokens(access: a, refresh: r);
  }

  @override
  Future<void> write(Tokens t) async {
    await _s.write(key: _a, value: t.access);
    await _s.write(key: _r, value: t.refresh);
  }

  @override
  Future<void> clear() async {
    await _s.delete(key: _a);
    await _s.delete(key: _r);
  }
}

/// In-memory store for tests.
class MemoryTokenStore implements TokenStore {
  Tokens? _t;
  @override
  Future<Tokens?> read() async => _t;
  @override
  Future<void> write(Tokens t) async => _t = t;
  @override
  Future<void> clear() async => _t = null;
}
