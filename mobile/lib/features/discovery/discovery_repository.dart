import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/models/json.dart' show jsonMaps, jsonString;
import '../../core/network/api_client.dart';
import '../../core/providers.dart';

class NearbyFilter {
  const NearbyFilter({
    this.radiusKm = 25,
    this.ageMin = 18,
    this.ageMax = 80,
    this.tags = const [],
    this.gender,
    this.goal,
    this.kids,
    this.drinking,
    this.smoking,
    this.exercise,
    this.language,
    this.verifiedOnly = false,
  });
  final String? goal;
  final String? kids;
  final String? drinking;
  final String? smoking;
  final String? exercise;
  final String? language;
  final bool verifiedOnly;

  /// How many optional filters are on (shown on the filter button).
  int get activeCount => [goal, kids, drinking, smoking, exercise, language, gender].where((v) => v != null).length + (verifiedOnly ? 1 : 0) + (tags.isNotEmpty ? 1 : 0);
  final int radiusKm;
  final int ageMin;
  final int ageMax;
  final List<String> tags;
  final String? gender;

  static const radii = [1, 2, 5, 10, 25, 50, 100];

  /// Pass an empty string to clear one of the optional filters.
  NearbyFilter copyWith({
    int? radiusKm,
    int? ageMin,
    int? ageMax,
    List<String>? tags,
    String? gender,
    bool clearGender = false,
    String? goal,
    String? kids,
    String? drinking,
    String? smoking,
    String? exercise,
    String? language,
    bool? verifiedOnly,
  }) {
    String? pick(String? n, String? old) => n == null ? old : (n.isEmpty ? null : n);
    return NearbyFilter(
      radiusKm: radiusKm ?? this.radiusKm,
      ageMin: ageMin ?? this.ageMin,
      ageMax: ageMax ?? this.ageMax,
      tags: tags ?? this.tags,
      gender: clearGender ? null : (gender ?? this.gender),
      goal: pick(goal, this.goal),
      kids: pick(kids, this.kids),
      drinking: pick(drinking, this.drinking),
      smoking: pick(smoking, this.smoking),
      exercise: pick(exercise, this.exercise),
      language: pick(language, this.language),
      verifiedOnly: verifiedOnly ?? this.verifiedOnly,
    );
  }

  Map<String, dynamic> toQuery() => {
        'radius_km': radiusKm,
        'age_min': ageMin,
        'age_max': ageMax,
        if (tags.isNotEmpty) 'tags': tags.join(','),
        if (gender != null) 'gender': gender,
        if (goal != null) 'goal': goal,
        if (kids != null) 'kids': kids,
        if (drinking != null) 'drinking': drinking,
        if (smoking != null) 'smoking': smoking,
        if (exercise != null) 'exercise': exercise,
        if (language != null) 'language': language,
        if (verifiedOnly) 'verified': 'true',
      };
}

class SwipeResult {
  const SwipeResult({required this.matched, this.matchId, this.theirComment = ''});
  final bool matched;
  final String? matchId;
  final String theirComment; // the note they sent with their earlier like
}

/// One of today's best people for you and why.
class TopPick {
  const TopPick({required this.profile, required this.why});
  final PublicProfile profile;
  final String why;
}

class LikesYou {
  const LikesYou({required this.count, required this.locked, required this.profiles, this.comments = const {}});
  final int count;
  final bool locked;
  final List<PublicProfile> profiles;

  /// Notes sent with a like, by the sender's profile ref.
  final Map<String, String> comments;
}

class DiscoveryRepository {
  DiscoveryRepository(this._api);
  final ApiClient _api;

  Future<List<PublicProfile>> nearby(NearbyFilter f) async =>
      listOf((await _api.getMap('/users/nearby', query: f.toQuery()))['results'], PublicProfile.fromJson);

  /// [comment] is an optional one-line note that goes with a like.
  Future<SwipeResult> swipe(String ref, String action, {String comment = ''}) async {
    final m = await _api.postMap('/matching/swipe', body: {'target_ref': ref, 'action': action, if (comment.isNotEmpty) 'comment': comment});
    return SwipeResult(matched: m['matched'] == true, matchId: m['match_id'] as String?, theirComment: (m['their_comment'] as String?) ?? '');
  }

  Future<SwipeResult> superLike(String ref, {String comment = ''}) async {
    final m = await _api.postMap('/matching/superlike', body: {'target_ref': ref, if (comment.isNotEmpty) 'comment': comment});
    return SwipeResult(matched: m['matched'] == true, matchId: m['match_id'] as String?, theirComment: (m['their_comment'] as String?) ?? '');
  }

  Future<void> boost() => _api.post('/matching/boost');

  /// Undoes the most recent pass (a Gold perk); the person returns to the deck.
  Future<void> rewind() => _api.post('/matching/rewind');

  Future<List<MatchView>> matches() async =>
      listOf((await _api.getMap('/matching/matches', query: {'limit': 50}))['matches'], MatchView.fromJson);

  Future<void> unmatch(String id) => _api.delete('/matching/matches/$id');

  /// True when both people have now revealed.
  Future<bool> reveal(String id) async => (await _api.postMap('/matching/matches/$id/reveal'))['mutual'] == true;

  Future<void> proposeRevealTimer(String id, int hours) => _api.post('/matching/matches/$id/reveal-timer', body: {'hours': hours});
  Future<void> cancelRevealTimer(String id) => _api.delete('/matching/matches/$id/reveal-timer');

  Future<void> revealVoice(String id) => _api.post('/matching/matches/$id/reveal-voice');

  Future<List<TopPick>> topPicks() async => [
        for (final m in jsonMaps((await _api.getMap('/matching/top-picks'))['picks']))
          TopPick(profile: PublicProfile.fromJson(Map<String, dynamic>.from(m['profile'] as Map? ?? {})), why: jsonString(m['why'])),
      ];

  Future<LikesYou> likesYou() async {
    final m = await _api.getMap('/matching/likes-you');
    return LikesYou(
      count: (m['count'] as num?)?.toInt() ?? 0,
      locked: m['locked'] == true,
      profiles: listOf(m['profiles'], PublicProfile.fromJson),
      comments: m['comments'] is Map ? {for (final e in (m['comments'] as Map).entries) '${e.key}': '${e.value}'} : const {},
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

final topPicksProvider = FutureProvider.autoDispose<List<TopPick>>((ref) => ref.watch(discoveryRepositoryProvider).topPicks());

final likesYouProvider = FutureProvider.autoDispose<LikesYou>((ref) => ref.watch(discoveryRepositoryProvider).likesYou());
