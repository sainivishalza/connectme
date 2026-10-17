import 'package:connectme/core/network/api_client.dart';
import 'package:connectme/core/network/token_store.dart';
import 'package:connectme/core/providers.dart';
import 'package:connectme/features/auth/auth_controller.dart';
import 'package:connectme/features/chat/chat_providers.dart';
import 'package:connectme/features/chat/crypto/secret_store.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

void main() {
  Future<(ProviderContainer, MemoryTokenStore, MemorySecretStore)> rig(Handler h) async {
    final tokens = MemoryTokenStore();
    await tokens.write(const Tokens(access: 'A', refresh: 'R'));
    final secrets = MemorySecretStore()..data.addAll({'signal.u1.identity': 'KEY', 'chat.u1.conv': 'MESSAGES'});
    final c = ProviderContainer(overrides: [
      apiClientProvider.overrideWithValue(fakeClient(h, tokens: tokens)),
      tokenStoreProvider.overrideWithValue(tokens),
      secretStoreProvider.overrideWithValue(secrets),
    ]);
    addTearDown(c.dispose);
    await c.read(authControllerProvider.notifier).restore();
    return (c, tokens, secrets);
  }

  test('a successful erase signs out and wipes tokens, keys and cached chats', () async {
    final (c, tokens, secrets) = await rig((o) async {
      expect(o.path, '/privacy/erase');
      expect(o.data, {'password': 'pw', 'confirm': 'DELETE'});
      return Reply(204);
    });
    await c.read(authControllerProvider.notifier).eraseAccount('pw');
    expect(c.read(authControllerProvider).status, AuthStatus.signedOut);
    expect(await tokens.read(), isNull);
    expect(secrets.data, isEmpty, reason: 'keys and message cache must be removed from the device');
  });

  test('a refused erase (wrong password) keeps the session, keys and chats', () async {
    final (c, tokens, secrets) = await rig((o) async => Reply(403, {'error': 'incorrect password'}));
    await expectLater(
      c.read(authControllerProvider.notifier).eraseAccount('nope'),
      throwsA(isA<ApiException>().having((e) => e.message, 'message', 'incorrect password')),
    );
    expect(c.read(authControllerProvider).status, AuthStatus.signedIn);
    expect(await tokens.read(), isNotNull);
    expect(secrets.data.length, 2, reason: 'nothing may be wiped when the server did not delete the account');
  });

  test('a server failure also keeps local data so the user can retry', () async {
    final (c, _, secrets) = await rig((o) async => Reply(500, {'error': 'could not complete erasure, please try again'}));
    await expectLater(c.read(authControllerProvider.notifier).eraseAccount('pw'), throwsA(isA<ApiException>()));
    expect(secrets.data.length, 2);
  });

  test('changing the password stores the fresh token pair and keeps the user signed in', () async {
    final (c, tokens, _) = await rig((o) async {
      expect(o.path, '/auth/change-password');
      expect(o.data, {'current_password': 'old', 'new_password': 'new-password-123'});
      return Reply(200, {'access_token': 'A2', 'refresh_token': 'R2'});
    });
    await c.read(authControllerProvider.notifier).changePassword('old', 'new-password-123');
    final t = await tokens.read();
    expect((t!.access, t.refresh), ('A2', 'R2'));
    expect(c.read(authControllerProvider).status, AuthStatus.signedIn);
  });

  test('a wrong current password keeps the old tokens', () async {
    final (c, tokens, _) = await rig((o) async => Reply(403, {'error': 'current password is incorrect'}));
    await expectLater(
      c.read(authControllerProvider.notifier).changePassword('bad', 'new-password-123'),
      throwsA(isA<ApiException>()),
    );
    expect((await tokens.read())!.refresh, 'R');
    expect(c.read(authControllerProvider).status, AuthStatus.signedIn);
  });

  test('sign out everywhere revokes server-side then clears this device', () async {
    final (c, tokens, _) = await rig((o) async {
      expect(o.path, '/auth/logout-all');
      return Reply(204);
    });
    await c.read(authControllerProvider.notifier).logoutEverywhere();
    expect(c.read(authControllerProvider).status, AuthStatus.signedOut);
    expect(await tokens.read(), isNull);
  });
}
