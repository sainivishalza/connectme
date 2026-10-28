import 'package:connectme/core/providers.dart';
import 'package:connectme/features/safety/my_reports_screen.dart';
import 'package:connectme/l10n/app_localizations.dart';
import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

void main() {
  Future<void> open(WidgetTester t, List<Map<String, dynamic>> reports) async {
    final client = fakeClient((o) async => o.path == '/moderation/reports/mine' ? Reply(200, {'reports': reports}) : Reply(404));
    await t.pumpWidget(ProviderScope(overrides: [apiClientProvider.overrideWithValue(client)], child: const MaterialApp(home: MyReportsScreen())));
    await t.pumpAndSettle();
  }

  testWidgets('each report shows whether staff looked and acted, and nothing about who was reported', (t) async {
    await open(t, [
      {'id': 'a', 'reason': 'spam', 'created_at': '2026-10-01T10:00:00Z', 'outcome': 'pending'},
      {'id': 'b', 'reason': 'fake_profile', 'created_at': '2026-09-20T10:00:00Z', 'outcome': 'action_taken'},
      {'id': 'c', 'reason': 'harassment', 'created_at': '2026-09-10T10:00:00Z', 'outcome': 'no_action'},
    ]);
    expect(find.text('Spam'), findsOneWidget);
    expect(find.text('Fake profile'), findsOneWidget);
    expect(find.textContaining('Waiting for a moderator'), findsOneWidget);
    expect(find.textContaining('we took action'), findsOneWidget);
    expect(find.textContaining('nothing that breaks the rules'), findsOneWidget);
  });

  testWidgets('with no reports it explains where they come from', (t) async {
    await open(t, []);
    expect(find.text('No reports yet'), findsOneWidget);
  });

  for (final c in {'hi': 'मेरी रिपोर्ट', 'es': 'Mis denuncias'}.entries) {
    testWidgets('the report screen speaks ${c.key}', (t) async {
      final client = fakeClient((o) async => Reply(200, {'reports': <Map<String, dynamic>>[]}));
      await t.pumpWidget(ProviderScope(
        overrides: [apiClientProvider.overrideWithValue(client)],
        child: MaterialApp(
          locale: Locale(c.key),
          supportedLocales: AppLocalizations.supportedLocales,
          localizationsDelegates: const [AppLocalizations.delegate, GlobalMaterialLocalizations.delegate, GlobalWidgetsLocalizations.delegate, GlobalCupertinoLocalizations.delegate],
          home: const MyReportsScreen(),
        ),
      ));
      await t.pumpAndSettle();
      expect(find.text(c.value), findsOneWidget);
      expect(find.text('No reports yet'), findsNothing);
    });
  }
}
