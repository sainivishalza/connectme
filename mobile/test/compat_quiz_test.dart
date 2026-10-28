import 'package:connectme/core/models.dart';
import 'package:connectme/core/providers.dart';
import 'package:connectme/features/discovery/widgets/profile_card.dart';
import 'package:connectme/features/quiz/screens/compat_quiz_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

Future<void> settle(WidgetTester t) async {
  await t.pumpAndSettle();
  await t.pump(const Duration(milliseconds: 100));
  await t.pumpAndSettle();
}

void main() {
  testWidgets('the quiz saves each answer, moves on, and counts progress', (t) async {
    t.view.physicalSize = const Size(800, 1800);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.reset);
    final saved = <Object?>[];
    final client = fakeClient((o) async {
      if (o.path == '/quiz/' && o.method == 'GET') {
        return Reply(200, {
          'questions': [
            {'id': 'weekend', 'text': 'How do you spend a weekend?', 'options': ['Out', 'Home']},
            {'id': 'plans', 'text': 'Plans or spontaneity?', 'options': ['Plan', 'Spontaneous']},
          ],
          'answers': <String, dynamic>{},
          'min_shared': 8,
        });
      }
      if (o.path == '/quiz/answers') {
        saved.add(o.data);
        return Reply(204);
      }
      return Reply(404);
    });
    await t.pumpWidget(ProviderScope(overrides: [apiClientProvider.overrideWithValue(client)], child: const MaterialApp(home: CompatQuizScreen())));
    await settle(t);
    expect(find.text('How do you spend a weekend?'), findsOneWidget);
    expect(find.text('0 of 2 answered'), findsOneWidget);
    expect(find.textContaining('Answer 8 more'), findsOneWidget);

    await t.tap(find.byKey(const Key('quiz_option_1')));
    await settle(t);
    expect(saved.single, {'answers': {'weekend': 1}});
    expect(find.text('Plans or spontaneity?'), findsOneWidget);
    expect(find.text('1 of 2 answered'), findsOneWidget);
  });

  testWidgets('a card shows the quiz match only when there is one', (t) async {
    PublicProfile p(int q) => PublicProfile(ref: 'r', anonymous: true, mode: 'incognito', quizMatch: q);
    await t.pumpWidget(MaterialApp(home: Scaffold(body: SingleChildScrollView(child: ProfileCard(profile: p(80))))));
    await t.pumpAndSettle();
    expect(find.text('80% quiz match'), findsOneWidget);
    await t.pumpWidget(MaterialApp(home: Scaffold(body: SingleChildScrollView(child: ProfileCard(profile: p(0))))));
    await t.pumpAndSettle();
    expect(find.byKey(const Key('quiz_badge')), findsNothing);
  });
}
