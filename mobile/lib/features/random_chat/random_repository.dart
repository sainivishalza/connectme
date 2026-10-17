import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/models/json.dart';
import '../../core/network/api_client.dart';
import '../../core/providers.dart';

/// A paired random chat as the server describes it.
class RandomSession {
  RandomSession({
    required this.matchId,
    required this.peer,
    this.expiresAt,
    this.keptByMe = false,
    this.kept = false,
    this.style = 'chat',
  });
  final String matchId;
  final PublicProfile peer;
  final DateTime? expiresAt; // null once both people kept it
  final bool keptByMe;
  final bool kept;
  final String style; // chat | truth

  bool get isTruth => style == 'truth';

  factory RandomSession.fromJson(Map<String, dynamic> j) => RandomSession(
    matchId: jsonString(j['match_id']),
    peer: PublicProfile.fromJson(
      Map<String, dynamic>.from(j['peer'] as Map? ?? {}),
    ),
    expiresAt: DateTime.tryParse(jsonString(j['expires_at'])),
    keptByMe: jsonBool(j['kept_by_me']),
    kept: jsonBool(j['kept']),
    style: jsonString(j['style']).isEmpty ? 'chat' : jsonString(j['style']),
  );
}

enum RandomState { idle, waiting, matched }

class RandomStatus {
  RandomStatus(this.state, [this.session]);
  final RandomState state;
  final RandomSession? session;

  factory RandomStatus.fromJson(Map<String, dynamic> j) {
    final state = switch (jsonString(j['state'])) {
      'waiting' => RandomState.waiting,
      'matched' => RandomState.matched,
      _ => RandomState.idle,
    };
    final s = j['session'];
    return RandomStatus(
      state,
      s is Map ? RandomSession.fromJson(Map<String, dynamic>.from(s)) : null,
    );
  }
}

class RandomRepository {
  RandomRepository(this._api);
  final ApiClient _api;

  Future<RandomStatus> join({String style = 'chat'}) async =>
      RandomStatus.fromJson(
        await _api.postMap(
          '/random/join',
          body: style == 'chat' ? null : {'style': style},
        ),
      );
  Future<RandomStatus> status() async =>
      RandomStatus.fromJson(await _api.getMap('/random/status'));
  Future<void> cancel() => _api.post('/random/cancel');
  Future<RandomSession> keep(String matchId) async =>
      RandomSession.fromJson(await _api.postMap('/random/$matchId/keep'));
}

final randomRepositoryProvider = Provider<RandomRepository>(
  (ref) => RandomRepository(ref.watch(apiClientProvider)),
);
