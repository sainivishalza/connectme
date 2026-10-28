import 'dart:convert';
import 'dart:io';

import 'package:connectme/core/settings/display_settings.dart';
import 'package:connectme/features/auth/screens/login_screen.dart';
import 'package:connectme/features/settings/appearance_screen.dart';
import 'package:connectme/l10n/app_localizations.dart';
import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'appearance_test.dart' show MemoryDisplayStore;

Map<String, dynamic> arb(String code) => jsonDecode(File('lib/l10n/app_$code.arb').readAsStringSync()) as Map<String, dynamic>;

void main() {
  test('every language has exactly the keys English has, none empty, and keeps any {placeholders}', () {
    final en = arb('en')..removeWhere((k, _) => k.startsWith('@'));
    for (final code in ['hi', 'es']) {
      final other = arb(code)..removeWhere((k, _) => k.startsWith('@'));
      expect(other.keys.toSet(), en.keys.toSet(), reason: '$code differs from en');
      for (final k in en.keys.where((k) => !k.startsWith('@'))) {
        expect((other[k] as String).trim(), isNotEmpty, reason: '$code.$k is empty');
        final holes = RegExp(r'\{(\w+)\}');
        expect(holes.allMatches(other[k] as String).map((m) => m.group(1)).toSet(), holes.allMatches(en[k] as String).map((m) => m.group(1)).toSet(), reason: '$code.$k placeholders');
      }
    }
  });

  test('the language list matches what the app can show', () {
    expect(AppLocalizations.supportedLocales.map((l) => l.languageCode).toSet(), DisplayPrefs.languages.toSet());
    expect(AppearanceScreen.languageNames.keys.toSet(), DisplayPrefs.languages.toSet());
  });

  testWidgets('choosing a language translates the screen at once, and system goes back to the phone language', (t) async {
    t.view.physicalSize = const Size(800, 2400);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.reset);
    final c = ProviderContainer(overrides: [displayStoreProvider.overrideWithValue(MemoryDisplayStore())]);
    addTearDown(c.dispose);
    await t.pumpWidget(UncontrolledProviderScope(
      container: c,
      child: Consumer(builder: (context, ref, _) {
        final p = ref.watch(displayPrefsProvider);
        return MaterialApp(
          locale: p.language == null ? const Locale('en') : Locale(p.language!),
          supportedLocales: AppLocalizations.supportedLocales,
          localizationsDelegates: const [AppLocalizations.delegate, GlobalMaterialLocalizations.delegate, GlobalWidgetsLocalizations.delegate, GlobalCupertinoLocalizations.delegate],
          home: const AppearanceScreen(),
        );
      }),
    ));
    await t.pumpAndSettle();
    expect(find.text('Appearance'), findsOneWidget);
    await t.tap(find.byKey(const Key('lang_hi')));
    await t.pumpAndSettle();
    expect(find.text('दिखावट'), findsOneWidget);
    expect(c.read(displayPrefsProvider).language, 'hi');
    await t.tap(find.byKey(const Key('lang_es')));
    await t.pumpAndSettle();
    expect(find.text('Apariencia'), findsOneWidget);
    await t.tap(find.byKey(const Key('lang_system')));
    await t.pumpAndSettle();
    expect(c.read(displayPrefsProvider).language, isNull);
    expect(find.text('Appearance'), findsOneWidget);
  });

  for (final c in [('hi', 'साइन इन करें', 'ईमेल'), ('es', 'Iniciar sesión', 'Correo electrónico')]) {
    testWidgets('the sign-in screen speaks ${c.$1}', (t) async {
      await t.pumpWidget(ProviderScope(
        child: MaterialApp(
          locale: Locale(c.$1),
          supportedLocales: AppLocalizations.supportedLocales,
          localizationsDelegates: const [AppLocalizations.delegate, GlobalMaterialLocalizations.delegate, GlobalWidgetsLocalizations.delegate, GlobalCupertinoLocalizations.delegate],
          home: const LoginScreen(),
        ),
      ));
      await t.pumpAndSettle();
      expect(find.text(c.$2), findsOneWidget);
      expect(find.text(c.$3), findsOneWidget);
      expect(find.text('Sign in'), findsNothing);
    });
  }
}
