import 'package:connectme/core/providers.dart';
import 'package:connectme/features/chapters/widgets/chapters_section.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_riverpod/misc.dart' show Override;
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

Map<String, dynamic> eventJson(String id, {String title = 'Rooftop mixer', int price = 0, int going = 3, int? capacity, bool joined = false, String kind = 'mixer', bool guests = true}) => {
      'id': id,
      'partner': 'Grand Hotel',
      'kind': kind,
      'title': title,
      'description': 'Drinks on the roof',
      'starts_at': DateTime.now().add(const Duration(days: 2)).toUtc().toIso8601String(),
      'ends_at': DateTime.now().add(const Duration(days: 2, hours: 3)).toUtc().toIso8601String(),
      'capacity': capacity,
      'going': going,
      'price_coins': price,
      'guests_only': guests,
      'joined': joined,
    };

Future<FakeAdapter> pump(WidgetTester t, Handler h, Widget home, {List<Override> extra = const []}) async {
  final client = fakeClient(h);
  await t.pumpWidget(ProviderScope(
    overrides: [apiClientProvider.overrideWithValue(client), ...extra],
    child: MaterialApp(home: home),
  ));
  await t.pumpAndSettle();
  return client.dio.httpClientAdapter as FakeAdapter;
}

void main() {
  testWidgets('chapters: add, edit and save, then readers see them', (t) async {
    Map? saved;
    await pump(t, (o) async {
      if (o.path == '/chapters/me' && o.method == 'GET') return Reply(200, {'chapters': []});
      if (o.path == '/chapters/me' && o.method == 'PUT') {
        saved = o.data as Map;
        return Reply(200, {'chapters': saved!['chapters']});
      }
      return Reply(404);
    }, const Scaffold(body: SingleChildScrollView(child: ChaptersSection())));
    expect(find.byKey(const Key('chapter_0')), findsNothing);
    for (var i = 0; i < 6; i++) {
      if (find.byKey(const Key('add_chapter')).evaluate().isNotEmpty) {
        await t.ensureVisible(find.byKey(const Key('add_chapter')));
        await t.tap(find.byKey(const Key('add_chapter')));
        await t.pump();
      }
    }
    expect(find.byKey(const Key('chapter_4')), findsOneWidget);
    expect(find.byKey(const Key('add_chapter')), findsNothing, reason: 'five chapters at most');
    await t.enterText(find.byKey(const Key('chapter_title_0')), 'The year I moved abroad');
    await t.enterText(find.byKey(const Key('chapter_body_0')), 'Two bags and a plant.');
    // Remove the four empty chapters after the first one.
    for (var n = 0; n < 4; n++) {
      t.widget<IconButton>(find.byKey(const Key('chapter_remove_1'))).onPressed!();
      await t.pump();
    }
    await t.ensureVisible(find.byKey(const Key('save_chapters')));
    await t.tap(find.byKey(const Key('save_chapters')));
    await t.pumpAndSettle();
    expect(saved?['chapters'], [
      {'title': 'The year I moved abroad', 'body': 'Two bags and a plant.'}
    ]);
  });
}
