import 'package:connectme/core/network/api_client.dart';
import 'package:connectme/core/network/token_store.dart';
import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

void main() {
  test('attaches the bearer token and parses JSON', () async {
    final tokens = MemoryTokenStore()..write(const Tokens(access: 'A1', refresh: 'R1'));
    String? seen;
    final c = fakeClient((o) async {
      seen = o.headers['Authorization'] as String?;
      return Reply(200, {'ok': true});
    }, tokens: tokens);
    expect(await c.getMap('/users/me'), {'ok': true});
    expect(seen, 'Bearer A1');
  });

  test('auth endpoints never carry a session token', () async {
    final tokens = MemoryTokenStore()..write(const Tokens(access: 'A1', refresh: 'R1'));
    Object? seen = 'unset';
    final c = fakeClient((o) async {
      seen = o.headers['Authorization'];
      return Reply(200, {});
    }, tokens: tokens);
    await c.postMap('/auth/login', body: {});
    expect(seen, isNull);
  });

  test('concurrent 401s trigger exactly one refresh, then all succeed', () async {
    final tokens = MemoryTokenStore()..write(const Tokens(access: 'OLD', refresh: 'R1'));
    var refreshCalls = 0;
    final c = fakeClient(
      (o) async {
        final auth = o.headers['Authorization'];
        return auth == 'Bearer NEW' ? Reply(200, {'path': o.path}) : Reply(401, {'error': 'expired'});
      },
      tokens: tokens,
      refresh: (o) async {
        refreshCalls++;
        await Future<void>.delayed(const Duration(milliseconds: 30));
        expect(o.data, {'refresh_token': 'R1'});
        return Reply(200, {'access_token': 'NEW', 'refresh_token': 'R2'});
      },
    );

    final results = await Future.wait([c.getMap('/a'), c.getMap('/b'), c.getMap('/c')]);
    expect(results.map((r) => r['path']), ['/a', '/b', '/c']);
    expect(refreshCalls, 1, reason: 'single-flight refresh');
    final t = await tokens.read();
    expect(t!.access, 'NEW');
    expect(t.refresh, 'R2');
  });

  test('a failed refresh signs the user out once', () async {
    final tokens = MemoryTokenStore()..write(const Tokens(access: 'OLD', refresh: 'R1'));
    var expired = 0;
    final c = fakeClient(
      (o) async => Reply(401, {'error': 'expired'}),
      tokens: tokens,
      refresh: (o) async => Reply(401, {'error': 'revoked'}),
      onExpired: () => expired++,
    );
    await expectLater(c.getMap('/users/me'), throwsA(isA<ApiException>().having((e) => e.isUnauthorized, 'unauthorized', true)));
    expect(expired, 1);
    expect(await tokens.read(), isNull);
  });

  test('a retried request is not retried again (no loop)', () async {
    final tokens = MemoryTokenStore()..write(const Tokens(access: 'OLD', refresh: 'R1'));
    final adapter = <String>[];
    final c = fakeClient(
      (o) async {
        adapter.add(o.headers['Authorization'] as String);
        return Reply(401, {'error': 'still no'});
      },
      tokens: tokens,
      refresh: (o) async => Reply(200, {'access_token': 'NEW', 'refresh_token': 'R2'}),
    );
    await expectLater(c.getMap('/x'), throwsA(isA<ApiException>()));
    expect(adapter, ['Bearer OLD', 'Bearer NEW']);
  });

  test('offline keeps the session and reports a network error', () async {
    final tokens = MemoryTokenStore()..write(const Tokens(access: 'A', refresh: 'R'));
    final dio = Dio(BaseOptions(baseUrl: 'http://test'))..httpClientAdapter = NetworkDown();
    final c = ApiClient(baseUrl: 'http://test', tokens: tokens, dio: dio);
    await expectLater(c.getMap('/x'), throwsA(isA<ApiException>().having((e) => e.isNetwork, 'network', true)));
    expect(await tokens.read(), isNotNull);
  });

  test('error bodies and Retry-After become ApiException', () async {
    final c = fakeClient((o) async => Reply(429, {'error': 'rate limit exceeded'}, {'Retry-After': '42'}));
    await expectLater(
      c.postMap('/matching/swipe'),
      throwsA(isA<ApiException>()
          .having((e) => e.message, 'message', 'rate limit exceeded')
          .having((e) => e.retryAfter, 'retryAfter', 42)
          .having((e) => e.isRateLimited, 'limited', true)),
    );
    final c2 = fakeClient((o) async => Reply(402, {'error': 'not enough coins'}));
    await expectLater(c2.postMap('/x'), throwsA(isA<ApiException>().having((e) => e.isPaymentRequired, 'payment', true)));
  });
}
