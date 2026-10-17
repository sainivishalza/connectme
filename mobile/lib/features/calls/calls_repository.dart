import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/models/json.dart';
import '../../core/network/api_client.dart';
import '../../core/providers.dart';

class CallInfo {
  CallInfo({required this.id, required this.matchId, required this.video, required this.status, required this.mine, this.peer});
  final String id;
  final String matchId;
  final bool video;
  final String status; // ringing | active | ended | declined | missed
  final bool mine; // I placed it
  final PublicProfile? peer;

  bool get over => status == 'ended' || status == 'declined' || status == 'missed';

  factory CallInfo.fromJson(Map<String, dynamic> j) => CallInfo(
        id: jsonString(j['id']),
        matchId: jsonString(j['match_id']),
        video: jsonBool(j['video']),
        status: jsonString(j['status']),
        mine: jsonBool(j['mine']),
        peer: j['peer'] is Map ? PublicProfile.fromJson(Map<String, dynamic>.from(j['peer'] as Map)) : null,
      );
}

class CallConnect {
  CallConnect({required this.url, required this.token, required this.room});
  final String url;
  final String token;
  final String room;

  factory CallConnect.fromJson(Map<String, dynamic> j) =>
      CallConnect(url: jsonString(j['url']), token: jsonString(j['token']), room: jsonString(j['room']));
}

/// A placed or answered call together with what is needed to join its room.
class StartedCall {
  StartedCall(this.call, this.connect);
  final CallInfo call;
  final CallConnect connect;

  factory StartedCall.fromJson(Map<String, dynamic> j) => StartedCall(
        CallInfo.fromJson(Map<String, dynamic>.from(j['call'] as Map)),
        CallConnect.fromJson(Map<String, dynamic>.from(j['connect'] as Map)),
      );
}

class CallsRepository {
  CallsRepository(this._api);
  final ApiClient _api;

  Future<StartedCall> start(String matchId, {required bool video}) async =>
      StartedCall.fromJson(await _api.postMap('/calls/', body: {'match_id': matchId, 'video': video}));

  /// The call ringing for me right now, if any.
  Future<CallInfo?> incoming() async {
    final c = (await _api.getMap('/calls/incoming'))['call'];
    return c is Map ? CallInfo.fromJson(Map<String, dynamic>.from(c)) : null;
  }

  Future<CallInfo> get(String id) async => CallInfo.fromJson(await _api.getMap('/calls/$id'));
  Future<StartedCall> accept(String id) async => StartedCall.fromJson(await _api.postMap('/calls/$id/accept'));
  Future<void> decline(String id) => _api.post('/calls/$id/decline');
  Future<void> end(String id) => _api.post('/calls/$id/end');
}

final callsRepositoryProvider = Provider<CallsRepository>((ref) => CallsRepository(ref.watch(apiClientProvider)));
