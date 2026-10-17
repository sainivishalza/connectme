import 'dart:convert';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../features/auth/auth_controller.dart';
import 'config.dart';
import 'network/api_client.dart';
import 'network/token_store.dart';

final tokenStoreProvider = Provider<TokenStore>((ref) => SecureTokenStore());

final apiClientProvider = Provider<ApiClient>((ref) {
  return ApiClient(
    baseUrl: AppConfig.apiBase,
    tokens: ref.watch(tokenStoreProvider),
    onSessionExpired: () => ref.read(authControllerProvider.notifier).sessionExpired(),
  );
});

/// The signed-in user's id, read from the access token's `sub` claim. Used
/// only to namespace on-device key material; the server never trusts it.
String? jwtSubject(String token) {
  try {
    final parts = token.split('.');
    if (parts.length != 3) return null;
    final json = utf8.decode(base64Url.decode(base64Url.normalize(parts[1])));
    return (jsonDecode(json) as Map)['sub'] as String?;
  } catch (_) {
    return null;
  }
}
