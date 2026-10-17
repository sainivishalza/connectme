import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models/json.dart';
import '../../core/network/api_client.dart';
import '../../core/providers.dart';

class LiveRoom {
  LiveRoom({required this.id, required this.title, required this.hostRef, required this.hostName, required this.mine});
  final String id;
  final String title;
  final String hostRef;
  final String hostName;
  final bool mine;

  factory LiveRoom.fromJson(Map<String, dynamic> j) => LiveRoom(
        id: jsonString(j['id']),
        title: jsonString(j['title']),
        hostRef: jsonString(j['host_ref']),
        hostName: jsonString(j['host_name']),
        mine: jsonBool(j['mine']),
      );
}

/// What the media client needs to connect to a room.
class LiveConnect {
  LiveConnect({required this.url, required this.token, required this.canPublish});
  final String url;
  final String token;
  final bool canPublish;

  factory LiveConnect.fromJson(Map<String, dynamic> j) =>
      LiveConnect(url: jsonString(j['url']), token: jsonString(j['token']), canPublish: jsonBool(j['can_publish']));
}

class LiveEvent {
  LiveEvent({required this.id, required this.kind, required this.name, required this.text, this.ref = ''});
  final int id;
  final String ref; // sender's public ref (for host mute/kick)
  final String kind; // chat | gift
  final String name;
  final String text;

  bool get isGift => kind == 'gift';

  factory LiveEvent.fromJson(Map<String, dynamic> j) =>
      LiveEvent(
          id: jsonInt(j['id']),
          ref: jsonString(j['ref']),
          kind: jsonString(j['kind']),
          name: jsonString(j['name']),
          text: jsonString(j['text']));
}

class StartedRoom {
  StartedRoom(this.room, this.connect);
  final LiveRoom room;
  final LiveConnect connect;
}

class GiftReceipt {
  GiftReceipt({required this.coins, required this.remaining, required this.replayed});
  final int coins;
  final int remaining;
  final bool replayed;
}

class LiveRepository {
  LiveRepository(this._api);
  final ApiClient _api;

  Future<List<LiveRoom>> rooms() async => listOf((await _api.getMap('/live/rooms'))['rooms'], LiveRoom.fromJson);

  Future<StartedRoom> start(String title) async {
    final m = await _api.postMap('/live/rooms', body: {'title': title});
    return StartedRoom(
      LiveRoom.fromJson(Map<String, dynamic>.from(m['room'] as Map)),
      LiveConnect.fromJson(Map<String, dynamic>.from(m['connect'] as Map)),
    );
  }

  Future<LiveConnect> join(String roomId) async => LiveConnect.fromJson(await _api.postMap('/live/rooms/$roomId/join'));

  /// [requestId] must be stable across retries of the same tap so a lost
  /// response can never charge twice.
  Future<GiftReceipt> gift(String roomId, {required String giftId, required String requestId}) async {
    final m = await _api.postMap('/live/rooms/$roomId/gift', body: {'gift_id': giftId, 'request_id': requestId});
    return GiftReceipt(coins: jsonInt(m['coins']), remaining: jsonInt(m['remaining']), replayed: jsonBool(m['replayed']));
  }

  Future<List<LiveEvent>> events(String roomId, {int after = 0}) async =>
      listOf((await _api.getMap('/live/rooms/$roomId/events', query: {'after': after}))['events'], LiveEvent.fromJson);

  Future<void> chat(String roomId, String text) => _api.post('/live/rooms/$roomId/chat', body: {'text': text});

  /// Host only. [action] is 'mute' (no chat) or 'kick' (no access).
  Future<void> moderate(String roomId, {required String ref, required String action}) =>
      _api.post('/live/rooms/$roomId/moderate', body: {'ref': ref, 'action': action});

  Future<void> end(String roomId) => _api.delete('/live/rooms/$roomId');
}

final liveRepositoryProvider = Provider<LiveRepository>((ref) => LiveRepository(ref.watch(apiClientProvider)));

final liveRoomsProvider =
    FutureProvider.autoDispose<List<LiveRoom>>((ref) => ref.watch(liveRepositoryProvider).rooms());
