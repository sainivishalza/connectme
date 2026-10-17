import 'package:flutter/foundation.dart' show kIsWeb;

/// Build-time configuration. Override with --dart-define, e.g.
/// flutter run --dart-define=API_BASE=https://api.example.com
///
/// On the web, when no address is defined at build time the app talks to the
/// origin it was loaded from (the dev preview server proxies the API and the
/// realtime relay on that one origin), so the same build works at whatever
/// address the host machine currently has.
class AppConfig {
  AppConfig._();

  static const _api = String.fromEnvironment('API_BASE');
  static const _realtime = String.fromEnvironment('REALTIME_BASE');

  /// 10.0.2.2 is the host machine as seen from the Android emulator.
  static String get apiBase => _api.isNotEmpty ? _api : (kIsWeb ? Uri.base.origin : 'http://10.0.2.2:8080');

  static String get realtimeBase => _realtime.isNotEmpty ? _realtime : (kIsWeb ? Uri.base.origin : 'http://10.0.2.2:3001');
}
