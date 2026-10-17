import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/providers.dart';
import '../auth/auth_controller.dart';
import 'crypto/crypto_service.dart';
import 'crypto/persistent_signal_store.dart';
import 'crypto/secret_store.dart';
import 'data/chat_repository.dart';
import 'data/chat_security.dart';
import 'data/message_cache.dart';
import 'data/realtime_client.dart';

final secretStoreProvider = Provider<SecretStore>((ref) => SecureSecretStore());

final _userIdProvider = Provider<String?>(
  (ref) => ref.watch(authControllerProvider.select((s) => s.userId)),
);

/// Null when signed out. Everything is namespaced by user id.
final chatSecurityProvider = Provider<ChatSecurity?>((ref) {
  final uid = ref.watch(_userIdProvider);
  if (uid == null) return null;
  final secrets = ref.watch(secretStoreProvider);
  return ChatSecurity(
    crypto: CryptoService(PersistentSignalStore(secrets, uid)),
    repo: ref.watch(chatRepositoryProvider),
    secrets: secrets,
    ns: uid,
  );
});

final messageCacheProvider = Provider<MessageCache?>((ref) {
  final uid = ref.watch(_userIdProvider);
  return uid == null ? null : MessageCache(ref.watch(secretStoreProvider), uid);
});

/// One socket per signed-in session; closed on sign-out.
final realtimeProvider = Provider<RealtimeClient?>((ref) {
  final uid = ref.watch(_userIdProvider);
  if (uid == null) return null;
  final c = SocketRealtimeClient(
    api: ref.watch(apiClientProvider),
    accessToken: () async =>
        (await ref.read(tokenStoreProvider).read())?.access,
  );
  ref.onDispose(c.dispose);
  c.connect();
  return c;
});
