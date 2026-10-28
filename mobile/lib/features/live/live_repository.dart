import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models/json.dart';
import '../../core/network/api_client.dart';
import '../../core/providers.dart';
import 'widgets/live_poll_card.dart';

class LiveRoom {
  LiveRoom({
    required this.id,
    required this.title,
    required this.hostRef,
    required this.hostName,
    required this.mine,
    this.following = false,
    this.topic = 'chat',
    this.theme = 'plain',
    this.pinned = '',
  });
  final String id;
  final String title;
  final String hostRef;
  final String hostName;
  final bool mine;
  final bool following; // the caller follows the host
  final String topic;
  final String theme;
  final String pinned;

  factory LiveRoom.fromJson(Map<String, dynamic> j) => LiveRoom(
    id: jsonString(j['id']),
    title: jsonString(j['title']),
    hostRef: jsonString(j['host_ref']),
    hostName: jsonString(j['host_name']),
    mine: jsonBool(j['mine']),
    following: jsonBool(j['following']),
    topic: jsonString(j['topic']).isEmpty ? 'chat' : jsonString(j['topic']),
    theme: jsonString(j['theme']).isEmpty ? 'plain' : jsonString(j['theme']),
    pinned: jsonString(j['pinned']),
  );
}

/// What the media client needs to connect to a room.
class LiveConnect {
  LiveConnect({
    required this.url,
    required this.token,
    required this.canPublish,
    this.cohost = false,
  });
  final bool cohost;
  final String url;
  final String token;
  final bool canPublish;

  factory LiveConnect.fromJson(Map<String, dynamic> j) => LiveConnect(
    url: jsonString(j['url']),
    token: jsonString(j['token']),
    canPublish: jsonBool(j['can_publish']),
    cohost: jsonBool(j['cohost']),
  );
}

class LiveEvent {
  LiveEvent({
    required this.id,
    required this.kind,
    required this.name,
    required this.text,
    this.ref = '',
  });
  final int id;
  final String ref; // sender's public ref (for host mute/kick)
  final String kind; // chat | gift
  final String name;
  final String text;

  bool get isGift => kind == 'gift';

  factory LiveEvent.fromJson(Map<String, dynamic> j) => LiveEvent(
    id: jsonInt(j['id']),
    ref: jsonString(j['ref']),
    kind: jsonString(j['kind']),
    name: jsonString(j['name']),
    text: jsonString(j['text']),
  );
}

/// Who is asking to come on camera, as the host sees it.
class StageAsk {
  const StageAsk({required this.ref, required this.name});
  final String ref;
  final String name;
}

/// The caller's place on the stage (the people on camera with the host), and
/// for the host or a co-host, the hands that are up.
class StageState {
  const StageState({this.me = 'none', this.requests = const [], this.guests = 0, this.max = 7});
  final String me; // none | requested | invited | on_stage
  final List<StageAsk> requests;
  final int guests;
  final int max;

  bool get onStage => me == 'on_stage';
  bool get invited => me == 'invited';
  bool get requested => me == 'requested';

  factory StageState.fromJson(Object? j) {
    if (j is! Map) return const StageState();
    final m = Map<String, dynamic>.from(j);
    return StageState(
      me: jsonString(m['me']).isEmpty ? 'none' : jsonString(m['me']),
      requests: [for (final r in jsonMaps(m['requests'])) StageAsk(ref: jsonString(r['ref']), name: jsonString(r['name']))],
      guests: jsonInt(m['guests']),
      max: jsonInt(m['max']) == 0 ? 7 : jsonInt(m['max']),
    );
  }

  @override
  bool operator ==(Object other) =>
      other is StageState && other.me == me && other.guests == guests && other.requests.map((r) => r.ref).join(',') == requests.map((r) => r.ref).join(',');

  @override
  int get hashCode => Object.hash(me, guests, requests.length);
}

class StartedRoom {
  StartedRoom(this.room, this.connect);
  final LiveRoom room;
  final LiveConnect connect;
}

class GiftReceipt {
  GiftReceipt({
    required this.coins,
    required this.remaining,
    required this.replayed,
  });
  final int coins;
  final int remaining;
  final bool replayed;
}

class LiveRepository {
  LiveRepository(this._api);
  final ApiClient _api;

  Future<List<LiveRoom>> rooms({
    String topic = '',
    bool followingOnly = false,
  }) async => listOf(
    (await _api.getMap(
      '/live/rooms',
      query: {
        if (topic.isNotEmpty) 'topic': topic,
        if (followingOnly) 'following': '1',
      },
    ))['rooms'],
    LiveRoom.fromJson,
  );

  Future<StartedRoom> start(String title, {String topic = 'chat', String theme = 'plain'}) async {
    final m = await _api.postMap(
      '/live/rooms',
      body: {'title': title, 'topic': topic, 'theme': theme},
    );
    return StartedRoom(
      LiveRoom.fromJson(Map<String, dynamic>.from(m['room'] as Map)),
      LiveConnect.fromJson(Map<String, dynamic>.from(m['connect'] as Map)),
    );
  }

  Future<LiveConnect> join(String roomId) async =>
      LiveConnect.fromJson(await _api.postMap('/live/rooms/$roomId/join'));

  /// [requestId] must be stable across retries of the same tap so a lost
  /// response can never charge twice.
  Future<GiftReceipt> gift(
    String roomId, {
    required String giftId,
    required String requestId,
  }) async {
    final m = await _api.postMap(
      '/live/rooms/$roomId/gift',
      body: {'gift_id': giftId, 'request_id': requestId},
    );
    return GiftReceipt(
      coins: jsonInt(m['coins']),
      remaining: jsonInt(m['remaining']),
      replayed: jsonBool(m['replayed']),
    );
  }

  Future<List<LiveEvent>> events(String roomId, {int after = 0}) async =>
      (await page(roomId, after: after)).events;

  /// New events plus the line the host has pinned right now.
  Future<({List<LiveEvent> events, String pinned, List<String> cohosts, LivePoll? poll, StageState stage})> page(
    String roomId, {
    int after = 0,
  }) async {
    final m = await _api.getMap(
      '/live/rooms/$roomId/events',
      query: {'after': after},
    );
    return (
      events: listOf(m['events'], LiveEvent.fromJson),
      pinned: jsonString(m['pinned']),
      cohosts: jsonStrings(m['cohosts']),
      poll: LivePoll.fromJson(m['poll']),
      stage: StageState.fromJson(m['stage']),
    );
  }

  /// Host or co-host: ask the room a question with 2 to 4 answers.
  Future<void> startPoll(String roomId, String question, List<String> options) =>
      _api.post('/live/rooms/$roomId/poll', body: {'question': question, 'options': options});
  Future<void> closePoll(String roomId) => _api.delete('/live/rooms/$roomId/poll');
  Future<void> votePoll(String roomId, String pollId, int choice) =>
      _api.post('/live/rooms/$roomId/poll/vote', body: {'poll_id': pollId, 'choice': choice});

  /// Host only. [action] is add or remove; at most two co-hosts.
  Future<void> cohost(String roomId, {required String ref, required String action}) =>
      _api.post('/live/rooms/$roomId/cohosts', body: {'ref': ref, 'action': action});

  /// Viewer: ask to come on camera. If the host already invited you, this accepts.
  Future<void> requestStage(String roomId) => _api.post('/live/rooms/$roomId/stage/request');

  /// Host or co-host: ask a member to come on camera.
  Future<void> inviteStage(String roomId, String ref) => _api.post('/live/rooms/$roomId/stage/invite', body: {'ref': ref});

  /// Host or co-host: let a raised hand up, or turn it down.
  Future<void> decideStage(String roomId, String ref, {required bool accept}) =>
      _api.post('/live/rooms/$roomId/stage/decide', body: {'ref': ref, 'accept': accept});

  /// Viewer: answer the host's invite.
  Future<void> respondStage(String roomId, {required bool accept}) =>
      _api.post('/live/rooms/$roomId/stage/respond', body: {'accept': accept});

  /// Withdraw a request, or step down from the stage.
  Future<void> leaveStage(String roomId) => _api.post('/live/rooms/$roomId/stage/leave');

  /// Host or co-host. Empty text clears the pin.
  Future<void> pin(String roomId, String text) =>
      _api.put('/live/rooms/$roomId/pin', body: {'text': text});

  Future<void> chat(String roomId, String text) =>
      _api.post('/live/rooms/$roomId/chat', body: {'text': text});

  /// Host only. [action] is 'mute' (no chat) or 'kick' (no access).
  Future<void> moderate(
    String roomId, {
    required String ref,
    required String action,
  }) => _api.post(
    '/live/rooms/$roomId/moderate',
    body: {'ref': ref, 'action': action},
  );

  Future<void> end(String roomId) => _api.delete('/live/rooms/$roomId');
}

final liveRepositoryProvider = Provider<LiveRepository>(
  (ref) => LiveRepository(ref.watch(apiClientProvider)),
);

/// What the live list is narrowed to: a topic ('' = all) and/or hosts you follow.
class LiveFilter {
  const LiveFilter({this.topic = '', this.followingOnly = false});
  final String topic;
  final bool followingOnly;
}

class LiveFilterNotifier extends Notifier<LiveFilter> {
  @override
  LiveFilter build() => const LiveFilter();
  void set(LiveFilter f) => state = f;
}

final liveFilterProvider = NotifierProvider<LiveFilterNotifier, LiveFilter>(
  LiveFilterNotifier.new,
);

const liveTopics = [
  'chat',
  'music',
  'talent',
  'gaming',
  'dating',
  'wellness',
  'learning',
];

String topicLabel(String t) =>
    t.isEmpty ? 'All' : '${t[0].toUpperCase()}${t.substring(1)}';

final liveRoomsProvider = FutureProvider.autoDispose<List<LiveRoom>>((ref) {
  final f = ref.watch(liveFilterProvider);
  return ref
      .watch(liveRepositoryProvider)
      .rooms(topic: f.topic, followingOnly: f.followingOnly);
});
