import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/models/json.dart';
import '../../core/network/api_client.dart';
import '../../core/providers.dart';

/// The three kinds of post.
enum PostKind {
  moment('moment', 'Moments'),
  universe('universe', 'Little Universe'),
  missed('missed', 'Missed connections'),
  community('community', 'Group');

  const PostKind(this.api, this.title);
  final String api;
  final String title;
}

class Post {
  Post({
    required this.id,
    required this.kind,
    required this.body,
    required this.createdAt,
    this.alias = '',
    this.author,
    this.distance = '',
    this.hotelName = '',
    this.mine = false,
    this.likes = 0,
    this.liked = false,
    this.claims = 0,
    this.claimed = false,
  });

  final String id;
  final String kind;
  final String body;
  final DateTime createdAt;
  final String alias; // universe: the made-up nickname
  final PublicProfile? author; // moments only
  final String distance;
  final String hotelName;
  final bool mine;
  final int likes;
  final bool liked;
  final int claims; // pending "that's me" on my own missed post
  final bool claimed; // I already said "that's me"

  factory Post.fromJson(Map<String, dynamic> j) => Post(
        id: jsonString(j['id']),
        kind: jsonString(j['kind']),
        body: jsonString(j['body']),
        createdAt: DateTime.tryParse(jsonString(j['created_at'])) ?? DateTime.now(),
        alias: jsonString(j['alias']),
        author: j['author'] is Map ? PublicProfile.fromJson(Map<String, dynamic>.from(j['author'] as Map)) : null,
        distance: jsonString(j['distance']),
        hotelName: jsonString(j['hotel_name']),
        mine: jsonBool(j['mine']),
        likes: jsonInt(j['likes']),
        liked: jsonBool(j['liked']),
        claims: jsonInt(j['claims']),
        claimed: jsonBool(j['claimed']),
      );

  Post copyWith({int? likes, bool? liked, bool? claimed}) => Post(
        id: id,
        kind: kind,
        body: body,
        createdAt: createdAt,
        alias: alias,
        author: author,
        distance: distance,
        hotelName: hotelName,
        mine: mine,
        likes: likes ?? this.likes,
        liked: liked ?? this.liked,
        claims: claims,
        claimed: claimed ?? this.claimed,
      );
}

class Claim {
  Claim({required this.id, required this.claimant});
  final String id;
  final PublicProfile claimant;

  factory Claim.fromJson(Map<String, dynamic> j) =>
      Claim(id: jsonString(j['id']), claimant: PublicProfile.fromJson(Map<String, dynamic>.from(j['claimant'] as Map? ?? {})));
}

class SocialRepository {
  SocialRepository(this._api);
  final ApiClient _api;

  Future<List<Post>> feed(PostKind kind, {String? before, int radiusKm = 10}) async => listOf(
        (await _api.getMap('/social/feed', query: {'kind': kind.api, 'radius_km': radiusKm, 'before': ?before}))['posts'],
        Post.fromJson,
      );

  Future<Post> create(PostKind kind, String body) async =>
      Post.fromJson(await _api.postMap('/social/posts', body: {'kind': kind.api, 'body': body}));

  Future<void> delete(String id) => _api.delete('/social/posts/$id');

  Future<({bool liked, int likes})> like(String id) async {
    final m = await _api.postMap('/social/posts/$id/like');
    return (liked: jsonBool(m['liked']), likes: jsonInt(m['likes']));
  }

  Future<void> report(String id, String reason) => _api.post('/social/posts/$id/report', body: {'reason': reason});

  Future<void> claim(String id) => _api.post('/social/posts/$id/claim');

  Future<List<Claim>> claims(String postId) async =>
      listOf((await _api.getMap('/social/posts/$postId/claims'))['claims'], Claim.fromJson);

  /// Returns the new match's id.
  Future<String> accept(String claimId) async =>
      jsonString((await _api.postMap('/social/claims/$claimId/accept'))['match_id']);

  Future<void> decline(String claimId) => _api.post('/social/claims/$claimId/decline');
}

final socialRepositoryProvider = Provider<SocialRepository>((ref) => SocialRepository(ref.watch(apiClientProvider)));
