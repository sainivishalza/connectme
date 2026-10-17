import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/network/api_client.dart';
import '../../core/providers.dart';

class NearbyFilter {
  const NearbyFilter({this.radiusKm = 25, this.ageMin = 18, this.ageMax = 80, this.tags = const [], this.gender});
  final int radiusKm;
  final int ageMin;
  final int ageMax;
  final List<String> tags;
  final String? gender;

  static const radii = [1, 2, 5, 10, 25, 50, 100];

  NearbyFilter copyWith({int? radiusKm, int? ageMin, int? ageMax, List<String>? tags, String? gender, bool clearGender = false}) =>
      NearbyFilter(
        radiusKm: radiusKm ?? this.radiusKm,
        ageMin: ageMin ?? this.ageMin,
        ageMax: ageMax ?? this.ageMax,
        tags: tags ?? this.tags,
        gender: clearGender ? null : (gender ?? this.gender),
      );

  Map<String, dynamic> toQuery() => {
        'radius_km': radiusKm,
        'age_min': ageMin,
        'age_max': ageMax,
        if (tags.isNotEmpty) 'tags': tags.join(','),
        if (gender != null) 'gender': gender,
      };
}

class SwipeResult {
  const SwipeResult({required this.matched, this.matchId});
  final bool matched;
  final String? matchId;
}

class LikesYou {
  const LikesYou({required this.count, required this.locked, required this.profiles});
  final int count;
  final bool locked;
  final List<PublicProfile> profiles;
}

class DiscoveryRepository {
  DiscoveryRepository(this._api);
  final ApiClient _api;

  Future<List<PublicProfile>> nearby(NearbyFilter f) async =>
      listOf((await _api.getMap('/users/nearby', query: f.toQuery()))['results'], PublicProfile.fromJson);

  Future<SwipeResult> swipe(String ref, String action) async {
    final m = await _api.postMap('/matching/swipe', body: {'target_ref': ref, 'action': action});
    return SwipeResult(matched: m['matched'] == true, matchId: m['match_id'] as String?);
  }

  Future<SwipeResult> superLike(String ref) async {
    final m = await _api.postMap('/matching/superlike', body: {'target_ref': ref});
    return SwipeResult(matched: m['matched'] == true, matchId: m['match_id'] as String?);
  }

  Future<void> boost() => _api.post('/matching/boost');

  Future<List<MatchView>> matches() async =>
      listOf((await _api.getMap('/matching/matches', query: {'limit': 50}))['matches'], MatchView.fromJson);

  Future<void> unmatch(String id) => _api.delete('/matching/matches/$id');

  /// True when both people have now revealed.
  Future<bool> reveal(String id) async => (await _api.postMap('/matching/matches/$id/reveal'))['mutual'] == true;

  Future<LikesYou> likesYou() async {
    final m = await _api.getMap('/matching/likes-you');
    return LikesYou(
      count: (m['count'] as num?)?.toInt() ?? 0,
      locked: m['locked'] == true,
      profiles: listOf(m['profiles'], PublicProfile.fromJson),
    );
  }

  Future<void> block(String ref) => _api.post('/moderation/blocks', body: {'ref': ref});

  Future<void> report(String ref, String reason, {String details = ''}) =>
      _api.post('/moderation/reports', body: {'ref': ref, 'reason': reason, 'details': details});
}

final discoveryRepositoryProvider = Provider<DiscoveryRepository>((ref) => DiscoveryRepository(ref.watch(apiClientProvider)));

class NearbyFilterNotifier extends Notifier<NearbyFilter> {
  @override
  NearbyFilter build() => const NearbyFilter();
  void set(NearbyFilter f) => state = f;
}

final nearbyFilterProvider = NotifierProvider<NearbyFilterNotifier, NearbyFilter>(NearbyFilterNotifier.new);

final nearbyProvider = FutureProvider.autoDispose<List<PublicProfile>>(
    (ref) => ref.watch(discoveryRepositoryProvider).nearby(ref.watch(nearbyFilterProvider)));

final matchesProvider =
    FutureProvider.autoDispose<List<MatchView>>((ref) => ref.watch(discoveryRepositoryProvider).matches());

final likesYouProvider = FutureProvider.autoDispose<LikesYou>((ref) => ref.watch(discoveryRepositoryProvider).likesYou());
