import 'package:connectme/core/providers.dart';
import 'package:connectme/features/hotel_mode/screens/hotel_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

Map<String, dynamic> guest(String ref, String mood) => {
      'ref': ref,
      'anonymous': true,
      'mode': 'hotel',
      'tags': <String>[],
      'distance': '',
      'hotel_name': 'Grand Hotel',
      if (mood.isNotEmpty) 'travel_mood': mood,
    };

void main() {
  testWidgets('hotel guests: set my mood, filter the list, and moods show on cards', (t) async {
    String? mine;
    final feedQueries = <String?>[];
    final client = fakeClient((o) async {
      if (o.path == '/geo/checkin' && o.method == 'GET') {
        return Reply(200, {
          'id': 's1',
          'hotel': {'id': 'h1', 'name': 'Grand Hotel'},
          'checked_in_at': DateTime.now().toUtc().toIso8601String(),
          'data_deleted_at': DateTime.now().add(const Duration(days: 4)).toUtc().toIso8601String(),
          if (mine != null && mine!.isNotEmpty) 'mood': mine,
        });
      }
      if (o.path == '/geo/checkin/mood') {
        mine = (o.data as Map)['mood'] as String;
        return Reply(204);
      }
      if (o.path == '/geo/hotels/h1/users') {
        final m = o.queryParameters['mood'] as String?;
        feedQueries.add(m);
        final all = [guest('r1', 'business'), guest('r2', 'guide')];
        return Reply(200, {
          'users': m == null ? all : all.where((g) => g['travel_mood'] == m).toList()
        });
      }
      return Reply(200, {'posts': [], 'drinks': []});
    });
    await t.binding.setSurfaceSize(const Size(500, 1400));
    addTearDown(() => t.binding.setSurfaceSize(null));
    await t.pumpWidget(ProviderScope(
      overrides: [apiClientProvider.overrideWithValue(client)],
      child: const MaterialApp(home: HotelScreen()),
    ));
    await t.pumpAndSettle();

    expect(find.text('On a business trip'), findsWidgets);
    expect(find.text('Happy to show you around'), findsWidgets);
    expect(feedQueries, [null]);

    // Filter to guides: the feed is requested with the mood.
    await t.tap(find.byKey(const Key('filter_guide')));
    await t.pumpAndSettle();
    expect(feedQueries.last, 'guide');

    // Pick my own mood.
    await t.tap(find.byKey(const Key('my_mood_company')));
    await t.pumpAndSettle();
    expect(mine, 'company');
    expect(t.widget<ChoiceChip>(find.byKey(const Key('my_mood_company'))).selected, isTrue);
    await t.tap(find.byKey(const Key('my_mood_company')));
    await t.pumpAndSettle();
    expect(mine, '', reason: 'tapping again clears it');

    await t.tap(find.byKey(const Key('filter_all')));
    await t.pumpAndSettle();
    expect(feedQueries.last, isNull);
  });
}
