import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models/json.dart';
import '../../core/network/api_client.dart';
import '../../core/providers.dart';

/// A daily window with no notifications except incoming calls. Times are
/// minutes after local midnight.
class QuietHours {
  const QuietHours({this.on = false, this.start = 22 * 60, this.end = 8 * 60});
  final bool on;
  final int start;
  final int end;

  QuietHours copyWith({bool? on, int? start, int? end}) => QuietHours(on: on ?? this.on, start: start ?? this.start, end: end ?? this.end);

  static String label(int minutes) {
    final h = minutes ~/ 60, m = minutes % 60;
    return '${h.toString().padLeft(2, '0')}:${m.toString().padLeft(2, '0')}';
  }
}

class PushPrefs {
  const PushPrefs({this.messages = true, this.matches = true, this.calls = true, this.bookings = true, this.live = true, this.comments = true, this.quiet = const QuietHours()});
  final QuietHours quiet;
  final bool messages;
  final bool matches;
  final bool calls;
  final bool bookings;
  final bool live;
  final bool comments;

  PushPrefs copyWith({bool? messages, bool? matches, bool? calls, bool? bookings, bool? live, bool? comments, QuietHours? quiet}) => PushPrefs(
        comments: comments ?? this.comments,
        quiet: quiet ?? this.quiet,
        messages: messages ?? this.messages,
        matches: matches ?? this.matches,
        calls: calls ?? this.calls,
        bookings: bookings ?? this.bookings,
        live: live ?? this.live,
      );

  factory PushPrefs.fromJson(Map<String, dynamic> j) => PushPrefs(
        messages: jsonBool(j['messages']),
        matches: jsonBool(j['matches']),
        calls: jsonBool(j['calls']),
        bookings: jsonBool(j['bookings']),
        live: j['live'] != false,
        comments: j['comments'] != false,
        quiet: j['quiet'] is Map
            ? QuietHours(
                on: jsonBool((j['quiet'] as Map)['on']),
                start: jsonInt((j['quiet'] as Map)['start']),
                end: jsonInt((j['quiet'] as Map)['end']),
              )
            : const QuietHours(),
      );

  Map<String, dynamic> toJson() => {'messages': messages, 'matches': matches, 'calls': calls, 'bookings': bookings,
        'live': live,
        'comments': comments,
        // The phone's current offset from UTC lets the server apply the window in local time.
        'quiet': {'on': quiet.on, 'start': quiet.start, 'end': quiet.end, 'tz_min': DateTime.now().timeZoneOffset.inMinutes},
      };
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
