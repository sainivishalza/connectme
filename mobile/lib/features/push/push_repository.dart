import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models/json.dart';
import '../../core/network/api_client.dart';
import '../../core/providers.dart';

class PushPrefs {
  const PushPrefs({this.messages = true, this.matches = true, this.calls = true, this.bookings = true});
  final bool messages;
  final bool matches;
  final bool calls;
  final bool bookings;

  PushPrefs copyWith({bool? messages, bool? matches, bool? calls, bool? bookings}) => PushPrefs(
        messages: messages ?? this.messages,
        matches: matches ?? this.matches,
        calls: calls ?? this.calls,
        bookings: bookings ?? this.bookings,
      );

  factory PushPrefs.fromJson(Map<String, dynamic> j) => PushPrefs(
        messages: jsonBool(j['messages']),
        matches: jsonBool(j['matches']),
        calls: jsonBool(j['calls']),
        bookings: jsonBool(j['bookings']),
      );

  Map<String, dynamic> toJson() => {'messages': messages, 'matches': matches, 'calls': calls, 'bookings': bookings};
}

class PushRepository {
  PushRepository(this._api);
  final ApiClient _api;

  Future<({PushPrefs prefs, int devices})> prefs() async {
    final m = await _api.getMap('/push/prefs');
    return (prefs: PushPrefs.fromJson(Map<String, dynamic>.from(m['prefs'] as Map? ?? {})), devices: jsonInt(m['devices']));
  }

  Future<void> setPrefs(PushPrefs p) => _api.put('/push/prefs', body: p.toJson());
  Future<void> register(String token, String platform) => _api.put('/push/devices', body: {'token': token, 'platform': platform});
  Future<void> unregister(String token) => _api.post('/push/devices/remove', body: {'token': token});
}

final pushRepositoryProvider = Provider<PushRepository>((ref) => PushRepository(ref.watch(apiClientProvider)));
