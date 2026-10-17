import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models/json.dart';
import '../../core/network/api_client.dart';
import '../../core/providers.dart';

class AudioRoom {
  AudioRoom({
    required this.id,
    required this.title,
    this.anonymous = false,
    this.speakers = 0,
    this.listeners = 0,
    this.mine = false,
  });
  final String id;
  final String title;
  final bool anonymous;
  final int speakers;
  final int listeners;
  final bool mine;

  factory AudioRoom.fromJson(Map<String, dynamic> j) => AudioRoom(
        id: jsonString(j['id']),
        title: jsonString(j['title']),
        anonymous: jsonBool(j['anonymous']),
        speakers: jsonInt(j['speakers']),
        listeners: jsonInt(j['listeners']),
        mine: jsonBool(j['mine']),
      );
}

class AudioMember {
  AudioMember({required this.id, required this.label, required this.role, this.handRaised = false, this.me = false});
  final String id;
  final String label;
  final String role; // host | speaker | listener
  final bool handRaised;
  final bool me;

  bool get onStage => role != 'listener';

  factory AudioMember.fromJson(Map<String, dynamic> j) => AudioMember(
        id: jsonString(j['id']),
        label: jsonString(j['label']),
        role: jsonString(j['role']),
        handRaised: jsonBool(j['hand_raised']),
        me: jsonBool(j['me']),
      );
}

class AudioConnect {
  AudioConnect({required this.url, required this.token, required this.canPublish, required this.memberId, required this.role});
  final String url;
  final String token;
  final bool canPublish;
  final String memberId;
  final String role;

  factory AudioConnect.fromJson(Map<String, dynamic> j) {
    final me = Map<String, dynamic>.from(j['me'] as Map? ?? {});
    return AudioConnect(
      url: jsonString(j['url']),
      token: jsonString(j['token']),
      canPublish: jsonBool(j['can_publish']),
      memberId: jsonString(me['id']),
      role: jsonString(me['role']),
    );
  }
}

class StartedAudio {
  StartedAudio(this.room, this.connect);
  final AudioRoom room;
  final AudioConnect connect;
}

class AudioState {
  AudioState(this.room, this.members, this.myRole);
  final AudioRoom room;
  final List<AudioMember> members;
  final String myRole;

  List<AudioMember> get stage => members.where((m) => m.onStage).toList();
  List<AudioMember> get audience => members.where((m) => !m.onStage).toList();
}

class AudioRepository {
  AudioRepository(this._api);
  final ApiClient _api;

  Future<List<AudioRoom>> rooms() async => listOf((await _api.getMap('/audio/rooms'))['rooms'], AudioRoom.fromJson);

  Future<StartedAudio> start(String title, {required bool anonymous}) async {
    final m = await _api.postMap('/audio/rooms', body: {'title': title, 'anonymous': anonymous});
    return StartedAudio(
      AudioRoom.fromJson(Map<String, dynamic>.from(m['room'] as Map)),
      AudioConnect.fromJson(Map<String, dynamic>.from(m['connect'] as Map)),
    );
  }

  Future<AudioConnect> join(String id) async => AudioConnect.fromJson(await _api.postMap('/audio/rooms/$id/join'));

  Future<AudioState> state(String id) async {
    final m = await _api.getMap('/audio/rooms/$id');
    final me = Map<String, dynamic>.from(m['me'] as Map? ?? {});
    return AudioState(
      AudioRoom.fromJson(Map<String, dynamic>.from(m['room'] as Map)),
      listOf(m['members'], AudioMember.fromJson),
      jsonString(me['role']),
    );
  }

  Future<void> hand(String id, {required bool up}) => _api.post('/audio/rooms/$id/hand', body: {'up': up});

  /// Host only: 'promote', 'demote' or 'kick'.
  Future<void> setRole(String id, String memberId, String action) =>
      _api.post('/audio/rooms/$id/members/$memberId', body: {'action': action});

  Future<void> leave(String id) => _api.post('/audio/rooms/$id/leave');
  Future<void> end(String id) => _api.delete('/audio/rooms/$id');
}

final audioRepositoryProvider = Provider<AudioRepository>((ref) => AudioRepository(ref.watch(apiClientProvider)));

final audioRoomsProvider = FutureProvider.autoDispose<List<AudioRoom>>((ref) => ref.watch(audioRepositoryProvider).rooms());
