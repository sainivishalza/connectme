import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/network/api_client.dart';
import '../../core/network/token_store.dart';
import '../../core/providers.dart';
import '../chat/chat_providers.dart';
import '../push/push_repository.dart';
import '../push/push_source.dart';

enum AuthStatus { unknown, signedOut, signedIn }

class AuthState {
  const AuthState(this.status, {this.userId, this.emailVerified = false});
  final AuthStatus status;
  final String? userId;
  final bool emailVerified;
}

final authControllerProvider =
    NotifierProvider<AuthController, AuthState>(AuthController.new);

class AuthController extends Notifier<AuthState> {
  @override
  AuthState build() {
    Future.microtask(restore);
    return const AuthState(AuthStatus.unknown);
  }

  ApiClient get _api => ref.read(apiClientProvider);
  TokenStore get _tokens => ref.read(tokenStoreProvider);

  /// Resumes a stored session on app start.
  Future<void> restore() async {
    final t = await _tokens.read();
    state = t == null
        ? const AuthState(AuthStatus.signedOut)
        : AuthState(AuthStatus.signedIn, userId: jwtSubject(t.access));
  }

  Future<void> login(String email, String password) async {
    final m = await _api.postMap('/auth/login', body: {'email': email.trim(), 'password': password});
    await _tokens.write(Tokens(access: m['access_token'] as String, refresh: m['refresh_token'] as String));
    state = AuthState(AuthStatus.signedIn,
        userId: jwtSubject(m['access_token'] as String), emailVerified: m['email_verified'] == true);
  }

  Future<void> register({
    required String email,
    required String password,
    required String dateOfBirth,
    String? phone,
  }) =>
      _api.post('/auth/register', body: {
        'email': email.trim(),
        'password': password,
        'date_of_birth': dateOfBirth,
        if (phone != null && phone.trim().isNotEmpty) 'phone': phone.trim(),
      });

  Future<void> verifyEmail(String token) => _api.post('/auth/verify-email', body: {'token': token.trim()});

  Future<void> forgotPassword(String email) => _api.post('/auth/forgot-password', body: {'email': email.trim()});

  Future<void> resetPassword(String token, String newPassword) =>
      _api.post('/auth/reset-password', body: {'token': token.trim(), 'new_password': newPassword});

  /// Changes the password. The server signs out every other device and returns
  /// a fresh token pair for this one, which replaces the stored session.
  Future<void> changePassword(String current, String next) async {
    final m = await _api.postMap('/auth/change-password',
        body: {'current_password': current, 'new_password': next});
    await _tokens.write(Tokens(access: m['access_token'] as String, refresh: m['refresh_token'] as String));
    await _registerPushDevice();
  }

  /// Revokes every session on every device, then clears this one too.
  Future<void> logoutEverywhere() async {
    await _api.post('/auth/logout-all'); // also makes the server forget every push device
    await _tokens.clear();
    state = const AuthState(AuthStatus.signedOut);
  }

  /// Tells the server to stop notifying this phone. Best effort, and done
  /// while we can still authenticate.
  Future<void> _forgetPushDevice() async {
    try {
      final src = ref.read(pushTokenSourceProvider);
      if (!src.available) return;
      final token = await src.token();
      if (token != null) await ref.read(pushRepositoryProvider).unregister(token);
    } catch (_) {}
  }

  /// The server forgets every device when other sessions are revoked, so the
  /// device that stays signed in registers again.
  Future<void> _registerPushDevice() async {
    try {
      final src = ref.read(pushTokenSourceProvider);
      if (!src.available || !await src.isAuthorized()) return;
      final token = await src.token();
      if (token != null) await ref.read(pushRepositoryProvider).register(token, src.platform);
    } catch (_) {}
  }

  Future<void> logout() async {
    await _forgetPushDevice();
    final t = await _tokens.read();
    if (t != null) {
      try {
        await _api.post('/auth/logout', body: {'refresh_token': t.refresh});
      } catch (_) {
        // Best effort: the local session is cleared regardless.
      }
    }
    await _tokens.clear();
    state = const AuthState(AuthStatus.signedOut);
  }

  /// Permanently deletes the account, then removes every key and cached
  /// message from this device. Throws (and keeps the session) if the server
  /// refuses, e.g. wrong password.
  Future<void> eraseAccount(String password) async {
    await _api.post('/privacy/erase', body: {'password': password, 'confirm': 'DELETE'});
    await _tokens.clear();
    await ref.read(secretStoreProvider).deleteAll();
    state = const AuthState(AuthStatus.signedOut);
  }

  /// Called by the API client when refresh fails for good.
  void sessionExpired() => state = const AuthState(AuthStatus.signedOut);
}
