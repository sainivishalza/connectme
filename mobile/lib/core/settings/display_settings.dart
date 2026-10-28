import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

/// How the app looks for this person on this device. Nothing here is sent to
/// the server.
@immutable
class DisplayPrefs {
  const DisplayPrefs({this.theme = ThemeMode.system, this.textScale = 1.0, this.reduceMotion = false, this.language});

  /// Two-letter language code chosen by the member, or null to follow the phone.
  final String? language;

  final ThemeMode theme;

  /// Multiplies the system text size (1.0, 1.15, 1.3 or 1.5).
  final double textScale;
  final bool reduceMotion;

  static const textScales = [1.0, 1.15, 1.3, 1.5];

  static const languages = ['en', 'hi', 'es'];

  DisplayPrefs copyWith({ThemeMode? theme, double? textScale, bool? reduceMotion, String? language, bool systemLanguage = false}) => DisplayPrefs(
        theme: theme ?? this.theme,
        textScale: textScale ?? this.textScale,
        reduceMotion: reduceMotion ?? this.reduceMotion,
        language: systemLanguage ? null : (language ?? this.language),
      );
}

abstract class DisplayStore {
  Future<DisplayPrefs> read();
  Future<void> write(DisplayPrefs p);
}

class SecureDisplayStore implements DisplayStore {
  SecureDisplayStore([FlutterSecureStorage? s]) : _s = s ?? const FlutterSecureStorage();
  final FlutterSecureStorage _s;

  @override
  Future<DisplayPrefs> read() async {
    try {
      final theme = await _s.read(key: 'ui_theme');
      final scale = double.tryParse(await _s.read(key: 'ui_text_scale') ?? '');
      final motion = await _s.read(key: 'ui_reduce_motion');
      final lang = await _s.read(key: 'ui_language');
      return DisplayPrefs(
        theme: ThemeMode.values.where((m) => m.name == theme).firstOrNull ?? ThemeMode.system,
        textScale: DisplayPrefs.textScales.contains(scale) ? scale! : 1.0,
        reduceMotion: motion == 'true',
        language: DisplayPrefs.languages.contains(lang) ? lang : null,
      );
    } catch (_) {
      return const DisplayPrefs(); // storage unavailable (private window, blocked): defaults still work
    }
  }

  @override
  Future<void> write(DisplayPrefs p) async {
    try {
      await _s.write(key: 'ui_theme', value: p.theme.name);
      await _s.write(key: 'ui_text_scale', value: '${p.textScale}');
      await _s.write(key: 'ui_reduce_motion', value: '${p.reduceMotion}');
      if (p.language == null) {
        await _s.delete(key: 'ui_language');
      } else {
        await _s.write(key: 'ui_language', value: p.language);
      }
    } catch (_) {
      // not saved; the choice still applies until the app closes
    }
  }
}

final displayStoreProvider = Provider<DisplayStore>((ref) => SecureDisplayStore());

class DisplayController extends Notifier<DisplayPrefs> {
  @override
  DisplayPrefs build() {
    Future.microtask(() async {
      final saved = await ref.read(displayStoreProvider).read();
      state = saved;
    });
    return const DisplayPrefs();
  }

  void set(DisplayPrefs next) {
    state = next;
    ref.read(displayStoreProvider).write(next);
  }
}

final displayPrefsProvider = NotifierProvider<DisplayController, DisplayPrefs>(DisplayController.new);

/// True when animations should be skipped: the phone asks for it or the member chose it.
bool reduceMotion(BuildContext context, DisplayPrefs prefs) => prefs.reduceMotion || MediaQuery.disableAnimationsOf(context);
