import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models/json.dart';
import '../../core/network/api_client.dart';
import '../../core/providers.dart';

class DigestRepository {
  DigestRepository(this._api);
  final ApiClient _api;

  Future<bool> enabled() async => jsonBool((await _api.getMap('/digest/me'))['enabled']);
  Future<void> set(bool on) => _api.put('/digest/me', body: {'enabled': on});
  Future<void> unsubscribe(String token) => _api.post('/digest/unsubscribe', body: {'token': token});
}

final digestRepositoryProvider = Provider<DigestRepository>((ref) => DigestRepository(ref.watch(apiClientProvider)));
