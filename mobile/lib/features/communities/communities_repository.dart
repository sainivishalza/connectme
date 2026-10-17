import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models/json.dart';
import '../../core/network/api_client.dart';
import '../../core/providers.dart';
import '../social/social_repository.dart';

class Community {
  Community({
    required this.id,
    required this.name,
    required this.kind,
    this.description = '',
    this.tag = '',
    this.hotelName = '',
    this.members = 0,
    this.joined = false,
    this.owner = false,
    this.distance = '',
  });

  final String id;
  final String name;
  final String kind; // interest | hotel
  final String description;
  final String tag;
  final String hotelName;
  final int members;
  final bool joined;
  final bool owner;
  final String distance;

  bool get isHotel => kind == 'hotel';

  factory Community.fromJson(Map<String, dynamic> j) => Community(
        id: jsonString(j['id']),
        name: jsonString(j['name']),
        kind: jsonString(j['kind']),
        description: jsonString(j['description']),
        tag: jsonString(j['tag']),
        hotelName: jsonString(j['hotel_name']),
        members: jsonInt(j['members']),
        joined: jsonBool(j['joined']),
        owner: jsonBool(j['owner']),
        distance: jsonString(j['distance']),
      );
}

class CommunitiesRepository {
  CommunitiesRepository(this._api);
  final ApiClient _api;

  Future<List<Community>> nearby() async => listOf((await _api.getMap('/communities/nearby'))['communities'], Community.fromJson);

  Future<Community> create({required String name, String description = '', String tag = ''}) async =>
      Community.fromJson(await _api.postMap('/communities/', body: {'name': name, 'description': description, 'tag': tag}));

  Future<Community> join(String id) async => Community.fromJson(await _api.postMap('/communities/$id/join'));
  Future<void> leave(String id) => _api.delete('/communities/$id/membership');
  Future<void> remove(String id) => _api.delete('/communities/$id');

  Future<List<Post>> posts(String id, {String? before}) async =>
      listOf((await _api.getMap('/communities/$id/posts', query: {'before': ?before}))['posts'], Post.fromJson);

  Future<Post> post(String id, String body) async => Post.fromJson(await _api.postMap('/communities/$id/posts', body: {'body': body}));
}

final communitiesRepositoryProvider = Provider<CommunitiesRepository>((ref) => CommunitiesRepository(ref.watch(apiClientProvider)));
