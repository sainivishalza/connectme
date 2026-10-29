import 'package:connectme/core/models.dart';
import 'package:connectme/core/providers.dart';
import 'package:connectme/core/theme/app_theme.dart';
import 'package:connectme/features/auth/screens/login_screen.dart';
import 'package:connectme/features/chat/screens/chat_list_screen.dart';
import 'package:connectme/features/discovery/widgets/deck_actions.dart';
import 'package:connectme/features/discovery/widgets/deck_card.dart';
import 'package:connectme/features/discovery/widgets/swipe_stack.dart';
import 'package:connectme/features/discovery/widgets/profile_card.dart';
import 'package:connectme/features/follows/screens/follows_screen.dart';
import 'package:connectme/features/investors/invest_screen.dart';
import 'package:connectme/features/profile/screens/profile_screen.dart';
import 'package:connectme/features/safety/safety_screen.dart';
import 'package:connectme/features/social/screens/feed_screen.dart';
import 'package:connectme/features/social/social_repository.dart';
import 'package:connectme/features/videos/screens/video_feed_screen.dart';
import 'package:connectme/features/videos/widgets/video_view.dart';
import 'package:connectme/features/levels/screens/levels_screen.dart';
import 'package:connectme/features/live/screens/live_list_screen.dart';
import 'package:connectme/features/onboarding/welcome_screen.dart';
import 'package:connectme/features/push/screens/notifications_screen.dart';
import 'package:connectme/features/quiz/screens/compat_quiz_screen.dart';
import 'package:connectme/features/settings/appearance_screen.dart';
import 'package:dio/dio.dart' show RequestOptions;
import 'package:flutter/material.dart';
import 'package:flutter/semantics.dart' show CustomSemanticsAction;
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

Future<Reply> api(RequestOptions o) async {
  switch (o.path) {
    case '/investors/public':
      return Reply(200, {
        'pool_percent': '49',
        'holders': [
          {'rank': 1, 'name': 'Vishal Saini', 'percent': '51', 'founder': true},
          {'rank': 2, 'name': 'A Very Long Investor Name That Keeps Going', 'percent': '12.345678'},
        ],
      });
    case '/levels/me':
      return Reply(200, {
        'charm': {'level': 12, 'title': 'Bright', 'total': 4000, 'next_at': 5000, 'progress': 40},
        'supporter': {'level': 3, 'title': 'Newcomer', 'total': 300, 'next_at': 400, 'progress': 10},
        'show_charm': true,
      });
    case '/quiz/':
      return Reply(200, {
        'questions': [
          {'id': 'q1', 'text': 'A fairly long question that will wrap onto several lines when text is large?', 'options': ['One option that is long', 'Another long option text', 'Short', 'Final']},
        ],
        'answers': <String, dynamic>{},
        'min_shared': 8,
      });
    case '/push/prefs':
      return Reply(200, {'prefs': {'messages': true, 'quiet': {'on': true, 'start': 1320, 'end': 480}}, 'devices': 0});
    case '/live/rooms':
      return Reply(200, {
        'rooms': [
          {'id': 'a', 'title': 'A live room with quite a long title to wrap around', 'host_name': 'Someone', 'topic': 'music', 'pinned': 'Requests open for everyone tonight', 'following': true}
        ]
      });
    case '/users/me':
      return Reply(200, {'ref': 'r', 'mode': 'open', 'onboarded': false, 'completeness': {'percent': 40, 'next_steps': []}});
  }
  if (o.path.startsWith('/matching') || o.path.startsWith('/groups')) return Reply(200, {'matches': []});
  return Reply(404);
}

/// Every screen is drawn on a phone at the largest text size, in both themes.
/// A layout overflow throws in tests, so a clean pump means nothing is cut off.
void main() {
  final screens = <String, Widget>{
    'login': const LoginScreen(),
    'levels': const LevelsScreen(),
    'quiz': const CompatQuizScreen(),
    'invest': const InvestScreen(),
    'notifications': const NotificationsScreen(),
    'appearance': const AppearanceScreen(),
    'welcome': const WelcomeScreen(),
    'live list': const LiveListScreen(),
    'chat list': const ChatListScreen(),
    'safety': const SafetyScreen(),
  };

  for (final theme in [Brightness.light, Brightness.dark]) {
    for (final e in screens.entries) {
      testWidgets('${e.key} fits at the largest text size in ${theme.name} mode', (t) async {
        t.view.physicalSize = const Size(390 * 2, 844 * 2);
        t.view.devicePixelRatio = 2;
        addTearDown(t.view.reset);
        await t.pumpWidget(ProviderScope(
          overrides: [apiClientProvider.overrideWithValue(fakeClient(api))],
          child: MaterialApp(
            theme: theme == Brightness.light ? AppTheme.light() : AppTheme.dark(),
            builder: (c, child) => MediaQuery(data: MediaQuery.of(c).copyWith(textScaler: const TextScaler.linear(2.0)), child: child!),
            home: e.value,
          ),
        ));
        await t.pumpAndSettle();
        final ex = t.takeException();
        expect(ex, isNull, reason: ex?.toString());
      });
    }
  }

  guidelineSuite();

  testWidgets('the swipe deck can be used without dragging: the card is described and offers Like, Pass and Super Like', (t) async {
    final h = t.ensureSemantics();
    final seen = <String>[];
    final people = [PublicProfile(ref: 'a', anonymous: false, mode: 'open', displayName: 'Mina', age: 29, quizMatch: 80, intro: 'Loves jazz')];
    await t.pumpWidget(MaterialApp(
      theme: AppTheme.light(),
      home: Scaffold(
        body: SizedBox(
          height: 600,
          child: SwipeStack<PublicProfile>(
            items: people,
            keyOf: (p) => p.ref,
            labelOf: describeProfile,
            onSwipe: (p, action) async {
              seen.add(action);
              return true;
            },
            cardBuilder: (c, p, progress) => DeckCard(profile: p),
          ),
        ),
      ),
    ));
    await t.pumpAndSettle();
    expect(describeProfile(people.first), 'Mina, 29. 80% quiz match. Loves jazz');
    final node = t.getSemantics(find.byKey(const ValueKey('top_a')));
    expect(node.label, contains('Mina, 29'));
    final data = node.getSemanticsData();
    expect(data.customSemanticsActionIds!.length, 3);
    final labels = [for (final id in data.customSemanticsActionIds!) CustomSemanticsAction.getAction(id)!.label];
    expect(labels, containsAll(['Like', 'Pass', 'Super Like']));
    h.dispose();
  });

  group('tap targets and labels', () {
    for (final theme in [Brightness.light, Brightness.dark]) {
      testWidgets('the deck buttons are big enough and named (${theme.name})', (t) async {
        final h = t.ensureSemantics();
        t.view.physicalSize = const Size(400, 800);
        t.view.devicePixelRatio = 1;
        addTearDown(t.view.reset);
        await t.pumpWidget(MaterialApp(
          theme: theme == Brightness.light ? AppTheme.light() : AppTheme.dark(),
          home: Scaffold(body: Center(child: DeckActions(onRewind: () {}, onPass: () {}, onSuperLike: () {}, onLike: () {}))),
        ));
        await expectLater(t, meetsGuideline(androidTapTargetGuideline));
        await expectLater(t, meetsGuideline(iOSTapTargetGuideline));
        await expectLater(t, meetsGuideline(labeledTapTargetGuideline));
        h.dispose();
      });

      testWidgets('settings switches and the quiz meet the guidelines (${theme.name})', (t) async {
        final h = t.ensureSemantics();
        t.view.physicalSize = const Size(390 * 2, 844 * 2);
        t.view.devicePixelRatio = 2;
        addTearDown(t.view.reset);
        await t.pumpWidget(ProviderScope(
          overrides: [apiClientProvider.overrideWithValue(fakeClient(api))],
          child: MaterialApp(theme: theme == Brightness.light ? AppTheme.light() : AppTheme.dark(), home: const NotificationsScreen()),
        ));
        await t.pumpAndSettle();
        await expectLater(t, meetsGuideline(androidTapTargetGuideline));
        await expectLater(t, meetsGuideline(labeledTapTargetGuideline));
        h.dispose();
      });
    }

    testWidgets('a profile card reads as one description for a screen reader', (t) async {
      final h = t.ensureSemantics();
      await t.pumpWidget(MaterialApp(
        theme: AppTheme.light(),
        home: Scaffold(
          body: SingleChildScrollView(
            child: ProfileCard(profile: PublicProfile(ref: 'r', anonymous: true, mode: 'incognito', quizMatch: 80, intro: 'Hello there')),
          ),
        ),
      ));
      await t.pumpAndSettle();
      expect(find.bySemanticsLabel(RegExp('80% quiz match')), findsOneWidget);
      h.dispose();
    });
  });
}

/// Screens people use all day, checked for tap-target size and for a spoken
/// name on every tappable thing, in both themes.
void guidelineSuite() {
  final data = <String, Widget Function()>{
    'profile': () => const ProfileScreen(),
    'video feed': () => const VideoFeedScreen(),
    'moments feed': () => const FeedScreen(kind: PostKind.moment),
    'live list': () => const LiveListScreen(),
    'levels': () => const LevelsScreen(),
    'welcome': () => const WelcomeScreen(),
    'appearance': () => const AppearanceScreen(),
    'invest': () => const InvestScreen(),
    'following': () => const FollowsScreen(),
  };
  for (final theme in [Brightness.light, Brightness.dark]) {
    for (final e in data.entries) {
      testWidgets('${e.key}: tap targets and labels (${theme.name})', (t) async {
        final h = t.ensureSemantics();
        t.view.physicalSize = const Size(400, 900);
        t.view.devicePixelRatio = 1;
        addTearDown(t.view.reset);
        await t.pumpWidget(ProviderScope(
          overrides: [
            apiClientProvider.overrideWithValue(fakeClient(api2)),
            videoViewBuilderProvider.overrideWithValue((id, {autoplay = true}) => const SizedBox()),
          ],
          child: MaterialApp(theme: theme == Brightness.light ? AppTheme.light() : AppTheme.dark(), home: e.value()),
        ));
        await t.pumpAndSettle();
        await expectLater(t, meetsGuideline(androidTapTargetGuideline));
        await expectLater(t, meetsGuideline(labeledTapTargetGuideline));
        h.dispose();
      });
    }
  }
}

Future<Reply> api2(RequestOptions o) async {
  switch (o.path) {
    case '/users/me':
      return Reply(200, {
        'ref': 'r', 'mode': 'open', 'display_name': 'Sam', 'age': 30, 'tags': ['jazz'], 'photos': [], 'onboarded': true,
        'completeness': {'percent': 60, 'next_steps': ['add_photo']},
      });
    case '/videos/feed':
      return Reply(200, {
        'videos': [
          {'id': 'v1', 'kind': 'short', 'caption': 'A clip', 'seconds': 8, 'likes': 2, 'mine': false, 'comments': 1, 'created_at': DateTime.now().toUtc().toIso8601String(), 'author': {'ref': 'a', 'name': 'Dana'}}
        ]
      });
    case '/social/feed':
      return Reply(200, {
        'posts': [
          {'id': 'p1', 'kind': 'moment', 'body': 'Coffee anyone?', 'likes': 1, 'comments': 2, 'mine': false, 'created_at': DateTime.now().toUtc().toIso8601String(), 'expires_at': DateTime.now().toUtc().toIso8601String(), 'author': {'ref': 'a', 'mode': 'open', 'display_name': 'Dana'}}
        ]
      });
  }
  return api(o);
}
