import 'package:connectme/core/network/api_client.dart';
import 'package:connectme/l10n/app_localizations.dart';
import 'package:connectme/shared/widgets/app_snack.dart';
import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

/// When the server cannot check public content it answers 503
/// moderation_unavailable. The person sees a friendly sentence in their
/// language, never the code.
Widget _app(ApiClient client, Locale locale) => MaterialApp(
      locale: locale,
      supportedLocales: AppLocalizations.supportedLocales,
      localizationsDelegates: const [
        AppLocalizations.delegate,
        GlobalMaterialLocalizations.delegate,
        GlobalWidgetsLocalizations.delegate,
        GlobalCupertinoLocalizations.delegate,
      ],
      home: Scaffold(
        body: Builder(
          builder: (context) => TextButton(
            key: const Key('save'),
            onPressed: () => guarded(context, () async {
              await client.request('PATCH', '/users/me', body: {'bio': 'hello'});
            }),
            child: const Text('Save'),
          ),
        ),
      ),
    );

void main() {
  final unavailable = fakeClient((o) async => Reply(503, {'error': 'moderation_unavailable'}, {'retry-after': '5'}));

  const messages = {
    'en': "We can't check this right now. Please try again in a moment.",
    'hi': 'हम अभी इसकी जाँच नहीं कर सकते। कृपया थोड़ी देर बाद फिर कोशिश करें।',
    'es': 'No podemos comprobarlo ahora mismo. Inténtalo de nuevo en un momento.',
  };

  for (final e in messages.entries) {
    testWidgets('moderation_unavailable shows a friendly message in ${e.key}', (t) async {
      await t.pumpWidget(_app(unavailable, Locale(e.key)));
      await t.tap(find.byKey(const Key('save')));
      await t.pumpAndSettle();
      expect(find.text(e.value), findsOneWidget);
      expect(find.textContaining('moderation_unavailable'), findsNothing);
    });
  }

  test('the 503 becomes an ApiException that knows what it is', () async {
    try {
      await unavailable.request('PATCH', '/users/me', body: {'bio': 'x'});
      fail('expected an ApiException');
    } on ApiException catch (e) {
      expect(e.status, 503);
      expect(e.isModerationUnavailable, isTrue);
      expect(e.retryAfter, 5);
      expect(e.message, moderationUnavailableMessage);
    }
  });

  test('other 503s are left alone', () async {
    final other = fakeClient((o) async => Reply(503, {'error': 'maintenance'}));
    try {
      await other.request('GET', '/users/me');
      fail('expected an ApiException');
    } on ApiException catch (e) {
      expect(e.isModerationUnavailable, isFalse);
      expect(e.message, 'maintenance');
    }
  });
}
