import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/network/api_client.dart';
import '../../core/providers.dart';

class FollowsRepository {
  FollowsRepository(this._api);
  final ApiClient _api;

  Future<void> follow(String ref) => _api.post('/follows/$ref');
  Future<void> unfollow(String ref) => _api.delete('/follows/$ref');

  /// [direction] is following or followers.
  Future<List<PublicProfile>> list(String direction) async =>
      listOf((await _api.getMap('/follows/$direction'))['profiles'], PublicProfile.fromJson);
}

final followsRepositoryProvider = Provider<FollowsRepository>((ref) => FollowsRepository(ref.watch(apiClientProvider)));

final followListProvider =
    FutureProvider.autoDispose.family<List<PublicProfile>, String>((ref, d) => ref.watch(followsRepositoryProvider).list(d));
