import 'package:connectme/core/models/profile.dart';
import 'package:connectme/core/providers.dart';
import 'package:connectme/features/profile/widgets/photo_manager.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

Future<(FakeAdapter, List<int>)> pump(WidgetTester t, List<OwnPhoto> photos) async {
  t.view.physicalSize = const Size(900, 900);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.reset);
  final client = fakeClient((o) async => Reply(o.method == 'GET' ? 404 : 204));
  final changed = <int>[];
  await t.pumpWidget(ProviderScope(
    overrides: [apiClientProvider.overrideWithValue(client)],
    child: MaterialApp(home: Scaffold(body: PhotoManager(photos: photos, onAdd: () {}, onChanged: () => changed.add(1)))),
  ));
  await t.pumpAndSettle();
  return (client.dio.httpClientAdapter as FakeAdapter, changed);
}

void main() {
  final photos = [OwnPhoto('p1', 'Sunday hike'), OwnPhoto('p2'), OwnPhoto('p3')];

  testWidgets('the first photo is marked main and captions show under each photo', (t) async {
    await pump(t, photos);
    expect(find.byKey(const Key('main_badge')), findsOneWidget);
    expect(find.text('Sunday hike'), findsOneWidget);
    expect(find.text('No caption'), findsNWidgets(2));
  });

  testWidgets('make main and move earlier send the new order', (t) async {
    final (a, changed) = await pump(t, photos);
    await t.tap(find.byKey(const Key('photo_menu_1')));
    await t.pumpAndSettle();
    await t.tap(find.text('Make main photo'));
    await t.pumpAndSettle();
    expect(a.requests.where((r) => r.method == 'PUT').single.path, '/users/me/photos/p2/main');

    await t.tap(find.byKey(const Key('photo_menu_2')));
    await t.pumpAndSettle();
    await t.tap(find.text('Move earlier'));
    await t.pumpAndSettle();
    final order = a.requests.last;
    expect(order.path, '/users/me/photos/order');
    expect(order.data, {'ids': ['p1', 'p3', 'p2']});
    expect(changed.length, 2);
  });

  testWidgets('the first photo can only move later; the last only earlier', (t) async {
    await pump(t, photos);
    await t.tap(find.byKey(const Key('photo_menu_0')));
    await t.pumpAndSettle();
    expect(find.text('Move later'), findsOneWidget);
    expect(find.text('Move earlier'), findsNothing);
    expect(find.text('Make main photo'), findsNothing);
  });

  testWidgets('a caption is edited in a dialog and saved', (t) async {
    final (a, _) = await pump(t, photos);
    await t.tap(find.byKey(const Key('photo_menu_1')));
    await t.pumpAndSettle();
    await t.tap(find.text('Edit caption'));
    await t.pumpAndSettle();
    await t.enterText(find.byKey(const Key('caption_field')), '  By the sea ');
    await t.tap(find.byKey(const Key('caption_save')));
    await t.pumpAndSettle();
    final put = a.requests.last;
    expect(put.path, '/users/me/photos/p2/caption');
    expect(put.data, {'caption': 'By the sea'});
  });
}
