import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/settings/display_settings.dart';
import '../../l10n/app_localizations.dart';

/// Appearance: light or dark, how large the text is, whether things move, and
/// the language. Saved on this device only. Strings come from the translation
/// files; where none is loaded (a bare test app) the English text is used.
class AppearanceScreen extends ConsumerWidget {
  const AppearanceScreen({super.key});

  static String scaleLabel(double s, [AppLocalizations? l]) => switch (s) {
        1.15 => l?.textSizeLarge ?? 'Large',
        1.3 => l?.textSizeLarger ?? 'Larger',
        1.5 => l?.textSizeLargest ?? 'Largest',
        _ => l?.textSizeDefault ?? 'Default',
      };

  /// Each language is named in itself, so people can find theirs.
  static const languageNames = {'en': 'English', 'hi': 'हिन्दी', 'es': 'Español'};

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = ref.watch(displayPrefsProvider);
    final c = ref.read(displayPrefsProvider.notifier);
    final t = Theme.of(context);
    final l = AppLocalizations.of(context);
    return Scaffold(
      appBar: AppBar(title: Text(l?.appearanceTitle ?? 'Appearance')),
      body: ListView(padding: const EdgeInsets.all(20), children: [
        Text(l?.appearanceTheme ?? 'Theme', style: t.textTheme.titleMedium),
        const SizedBox(height: 8),
        Wrap(key: const Key('theme_mode'), spacing: 8, runSpacing: 8, children: [
          for (final m in [
            (ThemeMode.system, l?.appearanceMatchPhone ?? 'Match phone', Icons.brightness_auto),
            (ThemeMode.light, l?.appearanceLight ?? 'Light', Icons.light_mode_outlined),
            (ThemeMode.dark, l?.appearanceDark ?? 'Dark', Icons.dark_mode_outlined),
          ])
            ChoiceChip(
              avatar: Icon(m.$3, size: 18),
              label: Text(m.$2),
              selected: p.theme == m.$1,
              onSelected: (_) => c.set(p.copyWith(theme: m.$1)),
            ),
        ]),
        const SizedBox(height: 24),
        Text(l?.appearanceLanguage ?? 'Language', style: t.textTheme.titleMedium),
        const SizedBox(height: 8),
        Wrap(key: const Key('language'), spacing: 8, runSpacing: 8, children: [
          ChoiceChip(
            key: const Key('lang_system'),
            label: Text(l?.appearanceLanguageSystem ?? 'Match phone'),
            selected: p.language == null,
            onSelected: (_) => c.set(p.copyWith(systemLanguage: true)),
          ),
          for (final code in DisplayPrefs.languages)
            ChoiceChip(
              key: Key('lang_$code'),
              label: Text(languageNames[code]!),
              selected: p.language == code,
              onSelected: (_) => c.set(p.copyWith(language: code)),
            ),
        ]),
        const SizedBox(height: 24),
        Text(l?.appearanceTextSize ?? 'Text size', style: t.textTheme.titleMedium),
        const SizedBox(height: 8),
        Wrap(key: const Key('text_scale'), spacing: 8, runSpacing: 8, children: [
          for (final v in DisplayPrefs.textScales)
            ChoiceChip(label: Text(scaleLabel(v, l)), selected: p.textScale == v, onSelected: (_) => c.set(p.copyWith(textScale: v))),
        ]),
        const SizedBox(height: 12),
        Card(
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Text('The quick brown fox jumps over the lazy dog. This is how text will look.', key: const Key('text_preview'), style: t.textTheme.bodyLarge),
          ),
        ),
        const SizedBox(height: 16),
        SwitchListTile(
          key: const Key('reduce_motion'),
          contentPadding: EdgeInsets.zero,
          title: Text(l?.appearanceReduceMotion ?? 'Reduce motion'),
          subtitle: Text(l?.appearanceReduceMotionHint ?? 'Less movement: no flying gifts, floating cards or springy swipes'),
          value: p.reduceMotion,
          onChanged: (v) => c.set(p.copyWith(reduceMotion: v)),
        ),
        const SizedBox(height: 8),
        Text(l?.appearanceSavedHere ?? 'These choices are saved on this device only.', style: t.textTheme.bodySmall),
      ]),
    );
  }
}
