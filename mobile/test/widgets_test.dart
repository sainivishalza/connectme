import 'package:connectme/core/models.dart';
import 'package:connectme/core/network/api_client.dart';
import 'package:connectme/features/discovery/widgets/profile_card.dart';
import 'package:connectme/features/payments/widgets/tier_card.dart';
import 'package:connectme/features/profile/widgets/mode_switch.dart';
import 'package:connectme/shared/widgets/app_button.dart';
import 'package:connectme/shared/widgets/states.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

Widget host(Widget child) => ProviderScope(child: MaterialApp(home: Scaffold(body: child)));

void main() {
  group('ProfileCard', () {
    testWidgets('anonymous card shows the lock chip and never a name', (t) async {
      final p = PublicProfile(ref: 'r', anonymous: true, mode: 'incognito', displayName: 'Secret Name', tags: ['hiking'], intro: 'coffee?', distance: 'within 5 km');
      await t.pumpWidget(host(ProfileCard(profile: p)));
      expect(find.text('Anonymous'), findsNWidgets(2)); // title + chip
      expect(find.text('Secret Name'), findsNothing);
      expect(find.text('coffee?'), findsOneWidget);
      expect(find.text('hiking'), findsOneWidget);
      expect(find.textContaining('within 5 km'), findsOneWidget);
    });

    testWidgets('full card shows name, age and VIP badge; actions fire', (t) async {
      var liked = 0, passed = 0, supered = 0;
      final p = PublicProfile(ref: 'r', anonymous: false, mode: 'open', displayName: 'Ana', age: 29, vip: true, distance: 'within 2 km');
      await t.pumpWidget(host(ProfileCard(profile: p, onLike: () => liked++, onPass: () => passed++, onSuperLike: () => supered++)));
      expect(find.text('Ana'), findsOneWidget);
      expect(find.textContaining('29'), findsOneWidget);
      expect(find.byIcon(Icons.workspace_premium), findsOneWidget);
      await t.tap(find.byKey(const Key('like')));
      await t.tap(find.byKey(const Key('pass')));
      await t.tap(find.byKey(const Key('superlike')));
      expect([liked, passed, supered], [1, 1, 1]);
    });

    testWidgets('a card without handlers shows no action buttons', (t) async {
      await t.pumpWidget(host(ProfileCard(profile: PublicProfile(ref: 'r', anonymous: false, mode: 'open', displayName: 'Bo'))));
      expect(find.byKey(const Key('like')), findsNothing);
    });
  });

  group('AsyncListView', () {
    Widget view(AsyncValue<List<String>> v, {VoidCallback? onRetry}) => host(AsyncListView<String>(
          value: v,
          onRefresh: () async => onRetry?.call(),
          emptyTitle: 'Nothing here',
          emptyMessage: 'Check back later',
          itemBuilder: (_, s) => ListTile(title: Text(s)),
        ));

    testWidgets('loading', (t) async {
      await t.pumpWidget(view(const AsyncLoading()));
      expect(find.byType(CircularProgressIndicator), findsOneWidget);
    });

    testWidgets('empty', (t) async {
      await t.pumpWidget(view(const AsyncData([])));
      expect(find.text('Nothing here'), findsOneWidget);
      expect(find.text('Check back later'), findsOneWidget);
    });

    testWidgets('data', (t) async {
      await t.pumpWidget(view(const AsyncData(['one', 'two'])));
      expect(find.text('one'), findsOneWidget);
      expect(find.text('two'), findsOneWidget);
    });

    testWidgets('error shows the API message and retries', (t) async {
      var retried = 0;
      await t.pumpWidget(view(AsyncError(ApiException(503, 'Service unavailable'), StackTrace.empty), onRetry: () => retried++));
      expect(find.text('Service unavailable'), findsOneWidget);
      await t.tap(find.text('Try again'));
      expect(retried, 1);
    });
  });

  group('ModeSwitch', () {
    testWidgets('reports a change between open and incognito', (t) async {
      String? chosen;
      await t.pumpWidget(host(ModeSwitch(mode: 'open', onChanged: (m) => chosen = m)));
      await t.tap(find.text('Incognito'));
      expect(chosen, 'incognito');
    });

    testWidgets('hotel mode shows incognito as forced and explains opting out', (t) async {
      await t.pumpWidget(host(ModeSwitch(mode: 'hotel', onChanged: (_) {})));
      expect(find.textContaining('Choose Open to opt out'), findsOneWidget);
      final seg = t.widget<SegmentedButton<String>>(find.byKey(const Key('mode_switch')));
      expect(seg.selected, {'incognito'});
    });

    testWidgets('is inert while busy', (t) async {
      var calls = 0;
      await t.pumpWidget(host(ModeSwitch(mode: 'open', busy: true, onChanged: (_) => calls++)));
      await t.tap(find.text('Incognito'));
      expect(calls, 0);
    });
  });

  group('TierCard / AppButton', () {
    testWidgets('current plan is not selectable', (t) async {
      var picked = 0;
      await t.pumpWidget(host(TierCard(info: tiers[1], current: true, onSelect: () => picked++)));
      expect(find.text('Current plan'), findsOneWidget);
      await t.tap(find.byKey(const Key('tier_gold')));
      expect(picked, 0);
    });

    testWidgets('other plans can be chosen', (t) async {
      var picked = 0;
      await t.pumpWidget(host(TierCard(info: tiers[2], current: false, onSelect: () => picked++)));
      await t.tap(find.text('Choose Platinum'));
      expect(picked, 1);
    });

    testWidgets('busy button ignores taps and shows progress', (t) async {
      var taps = 0;
      await t.pumpWidget(host(AppButton(label: 'Go', busy: true, onPressed: () => taps++)));
      expect(find.byType(CircularProgressIndicator), findsOneWidget);
      await t.tap(find.byType(FilledButton));
      expect(taps, 0);
    });
  });
}
