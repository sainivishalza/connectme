import 'package:connectme/core/models.dart';
import 'package:connectme/core/providers.dart';
import 'package:connectme/features/discovery/discovery_repository.dart';
import 'package:connectme/features/discovery/widgets/filter_sheet.dart';
import 'package:connectme/features/discovery/widgets/profile_card.dart';
import 'package:connectme/features/profile/screens/about_you_screen.dart';
import 'package:connectme/features/profile/screens/prompts_screen.dart';
import 'package:connectme/features/profile/widgets/completeness_card.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';

import 'helpers.dart';

Map<String, dynamic> me({Map<String, dynamic> details = const {}, List<Map<String, dynamic>> prompts = const [], int percent = 30, List<String> next = const ['set_goal']}) => {
      'ref': 'r1',
      'mode': 'open',
      'display_name': 'Ana',
      'age': 29,
      'tags': <String>[],
      'photos': <Map<String, dynamic>>[],
      'details': details,
      'prompts': prompts,
      'completeness': {'percent': percent, 'next_steps': next},
    };

Future<FakeAdapter> pump(WidgetTester t, Handler h, Widget home, {bool pushed = false}) async {
  t.view.physicalSize = const Size(800, 2400);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.reset);
  final client = fakeClient(h);
  final router = GoRouter(routes: [
    GoRoute(path: '/', builder: (_, _) => pushed ? const Scaffold(body: Text('ROOT')) : home),
    GoRoute(path: '/screen', builder: (_, _) => home),
    GoRoute(path: '/profile/about', builder: (_, _) => const Scaffold(body: Text('ABOUT PAGE'))),
    GoRoute(path: '/profile/edit', builder: (_, _) => const Scaffold(body: Text('EDIT PAGE'))),
  ]);
  await t.pumpWidget(ProviderScope(overrides: [apiClientProvider.overrideWithValue(client)], child: MaterialApp.router(routerConfig: router)));
  await t.pumpAndSettle();
  if (pushed) {
    router.push('/screen');
    await t.pumpAndSettle();
  }
  return client.dio.httpClientAdapter as FakeAdapter;
}

void main() {
  testWidgets('About you saves only what the member chose, and tapping a chosen chip clears it', (t) async {
    Map? saved;
    final adapter = await pump(t, (o) async {
      if (o.path == '/users/me' && o.method == 'GET') return Reply(200, me(details: {'goal': 'casual', 'smoking': 'never'}));
      if (o.path == '/users/me/details') {
        saved = o.data as Map;
        return Reply(200, {'goal': 'long_term'});
      }
      return Reply(404);
    }, const AboutYouScreen(), pushed: true);
    // Existing answers are pre-selected.
    expect(t.widget<ChoiceChip>(find.byKey(const Key('goal_casual'))).selected, isTrue);
    await t.tap(find.byKey(const Key('goal_long_term')));
    await t.tap(find.byKey(const Key('smoking_never'))); // tap the chosen one again: clear it
    await t.tap(find.byKey(const Key('kids_open')));
    await t.pump();
    await t.enterText(find.byKey(const Key('job')), 'Nurse');
    await t.ensureVisible(find.byKey(const Key('save_about')));
    await t.tap(find.byKey(const Key('save_about')));
    await t.pumpAndSettle();
    expect(saved!['goal'], 'long_term');
    expect(saved!['smoking'], '');
    expect(saved!['kids'], 'open');
    expect(saved!['job_title'], 'Nurse');
    expect(adapter.count('/users/me/details'), 1);
  });

  testWidgets('prompts: choose a question, answer it, and a question cannot be picked twice', (t) async {
    Map? saved;
    await pump(t, (o) async {
      if (o.path == '/users/me' && o.method == 'GET') {
        return Reply(200, me(prompts: [
          {'key': 'geek_out', 'prompt': 'I could talk for hours about', 'answer': 'Maps'}
        ]));
      }
      if (o.path == '/users/prompts') {
        return Reply(200, {
          'prompts': [
            {'key': 'geek_out', 'text': 'I could talk for hours about'},
            {'key': 'fun_fact', 'text': 'A fun fact about me is'},
          ]
        });
      }
      if (o.path == '/users/me/prompts') {
        saved = o.data as Map;
        return Reply(200, {'prompts': []});
      }
      return Reply(404);
    }, const PromptsScreen(), pushed: true);
    expect(find.text('I could talk for hours about'), findsOneWidget);
    await t.tap(find.byKey(const Key('slot_1')));
    await t.pumpAndSettle();
    // The question already used in slot 0 is disabled.
    expect(t.widget<ListTile>(find.byKey(const Key('choose_geek_out'))).enabled, isFalse);
    await t.tap(find.byKey(const Key('choose_fun_fact')));
    await t.pumpAndSettle();
    await t.enterText(find.byKey(const Key('answer_1')), 'I juggle');
    await t.tap(find.byKey(const Key('save_prompts')));
    await t.pumpAndSettle();
    expect((saved!['prompts'] as List).map((p) => p['key']), ['geek_out', 'fun_fact']);
    expect((saved!['prompts'] as List).last['answer'], 'I juggle');
  });

  testWidgets('a full card shows the badge, lifestyle chips and prompts; an anonymous one only what it was sent', (t) async {
    final full = PublicProfile.fromJson({
      'ref': 'a', 'mode': 'open', 'anonymous': false, 'display_name': 'Ana', 'age': 29, 'tags': ['jazz'], 'id_verified': true,
      'details': {'goal': 'long_term', 'job_title': 'Nurse', 'height_cm': 178, 'languages': ['en', 'pt']},
      'prompts': [{'key': 'perfect_sunday', 'prompt': 'My perfect Sunday is', 'answer': 'Market, long lunch'}],
    });
    final anon = PublicProfile.fromJson({
      'ref': 'b', 'mode': 'incognito', 'anonymous': true, 'tags': ['jazz'], 'intro': 'hi', 'id_verified': true,
      'details': {'goal': 'casual', 'smoking': 'never'},
    });
    await t.pumpWidget(ProviderScope(
      child: MaterialApp(home: Scaffold(body: ListView(children: [ProfileCard(key: const Key('full'), profile: full), ProfileCard(key: const Key('anon'), profile: anon)]))),
    ));
    await t.pumpAndSettle();
    final fullCard = find.byKey(const Key('full'));
    expect(find.descendant(of: fullCard, matching: find.byKey(const Key('verified_badge'))), findsOneWidget);
    for (final s in ['Long-term relationship', '178 cm', 'Nurse', 'English, Portuguese', 'My perfect Sunday is', 'Market, long lunch']) {
      expect(find.descendant(of: fullCard, matching: find.text(s)), findsOneWidget, reason: s);
    }
    final anonCard = find.byKey(const Key('anon'));
    expect(find.descendant(of: anonCard, matching: find.byKey(const Key('verified_badge'))), findsOneWidget);
    expect(find.descendant(of: anonCard, matching: find.text('Something casual')), findsOneWidget);
    expect(find.descendant(of: anonCard, matching: find.text('Smokes: Never')), findsOneWidget);
    expect(find.descendant(of: anonCard, matching: find.textContaining('Nurse')), findsNothing);
  });

  testWidgets('the completeness card shows progress and the next step, and takes you there', (t) async {
    await pump(t, (o) async => Reply(404),
        Scaffold(body: CompletenessCard(completeness: const Completeness(percent: 45, nextSteps: ['set_goal', 'answer_prompts']))));
    expect(find.text('45%'), findsOneWidget);
    expect(find.text('Say what you are looking for'), findsOneWidget);
    expect(find.text('1 more after that'), findsOneWidget);
    await t.tap(find.byKey(const Key('completeness')));
    await t.pumpAndSettle();
    expect(find.text('ABOUT PAGE'), findsOneWidget);
    expect(stepRoute('add_photo'), '/profile/edit');
    expect(stepRoute('answer_prompts'), '/profile/prompts');
    // A finished profile shows nothing.
    await pump(t, (o) async => Reply(404), const Scaffold(body: CompletenessCard(completeness: Completeness(percent: 100))));
    expect(find.byKey(const Key('completeness')), findsNothing);
  });

  testWidgets('filters: turn on what they want and verified-only, and the query carries it; clearing keeps age and distance', (t) async {
    late ProviderContainer container;
    await t.pumpWidget(ProviderScope(
      child: Consumer(builder: (c, ref, _) {
        container = ProviderScope.containerOf(c);
        return const MaterialApp(home: Scaffold(body: FilterSheet()));
      }),
    ));
    t.view.physicalSize = const Size(800, 2400);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.reset);
    await t.pumpAndSettle();
    await t.tap(find.byKey(const Key('f_goal_long_term')));
    await t.tap(find.byKey(const Key('f_smoking_never')));
    await t.tap(find.byKey(const Key('f_verified')));
    await t.pump();
    await t.ensureVisible(find.byKey(const Key('apply_filters')));
    await t.tap(find.byKey(const Key('apply_filters')));
    await t.pumpAndSettle();
    final f = container.read(nearbyFilterProvider);
    expect(f.activeCount, 3);
    expect(f.toQuery(), containsPair('goal', 'long_term'));
    expect(f.toQuery(), containsPair('smoking', 'never'));
    expect(f.toQuery(), containsPair('verified', 'true'));
    expect(f.toQuery().containsKey('kids'), isFalse);
    // copyWith('') clears one filter and leaves the rest.
    final cleared = f.copyWith(goal: '');
    expect(cleared.goal, isNull);
    expect(cleared.smoking, 'never');
  });
}
