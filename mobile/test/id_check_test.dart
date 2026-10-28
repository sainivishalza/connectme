import 'dart:typed_data';

import 'package:connectme/core/providers.dart';
import 'package:connectme/features/profile/screens/id_check_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_riverpod/misc.dart' show Override;
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

Future<FakeAdapter> pump(WidgetTester t, Handler h, {List<Override> extra = const []}) async {
  final client = fakeClient(h);
  await t.pumpWidget(ProviderScope(
    overrides: [apiClientProvider.overrideWithValue(client), ...extra],
    child: const MaterialApp(home: IdCheckScreen()),
  ));
  await t.pumpAndSettle();
  return client.dio.httpClientAdapter as FakeAdapter;
}

void main() {
  testWidgets('walk through: get a code, take both photos, send, then wait for review', (t) async {
    t.view.physicalSize = const Size(800, 2000);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.reset);
    var state = 'none';
    var submitted = 0;
    final taken = <bool>[];
    await pump(t, (o) async {
      if (o.path == '/idcheck/status') return Reply(200, {'state': state, if (state == 'awaiting') 'code': '4821'});
      if (o.path == '/idcheck/start') {
        state = 'awaiting';
        return Reply(200, {'state': 'awaiting', 'code': '4821', 'expires_at': DateTime.now().add(const Duration(minutes: 30)).toUtc().toIso8601String()});
      }
      if (o.path == '/idcheck/submit') {
        submitted++;
        state = 'pending';
        return Reply(200, {'state': 'pending'});
      }
      return Reply(404);
    }, extra: [
      photoPickerProvider.overrideWithValue(({required bool selfie}) async {
        taken.add(selfie);
        return Uint8List.fromList([1, 2, 3]);
      }),
    ]);

    expect(find.text('Get my code'), findsOneWidget);
    await t.tap(find.byKey(const Key('start_id')));
    await t.pumpAndSettle();
    expect(find.byKey(const Key('id_code')), findsOneWidget);
    expect(find.text('4821'), findsOneWidget);

    // Sending needs both photos.
    expect(t.widget<FilledButton>(find.byKey(const Key('send_id'))).onPressed, isNull);
    await t.tap(find.byKey(const Key('take_id')));
    await t.pumpAndSettle();
    expect(t.widget<FilledButton>(find.byKey(const Key('send_id'))).onPressed, isNull);
    await t.tap(find.byKey(const Key('take_selfie')));
    await t.pumpAndSettle();
    expect(taken, [false, true]); // the ID with the rear camera, the selfie with the front one
    await t.tap(find.byKey(const Key('send_id')));
    await t.pumpAndSettle();
    expect(submitted, 1);
    expect(find.text('We are checking your photos'), findsOneWidget);
  });

  testWidgets('a rejection explains why and offers another try; verified shows the badge screen', (t) async {
    await pump(t, (o) async => Reply(200, {'state': 'rejected', 'reject_reason': 'face_mismatch'}));
    expect(find.textContaining('did not match the ID'), findsOneWidget);
    expect(find.text('Try again'), findsOneWidget);

  });

  testWidgets('verified members see a thank-you and no way to resubmit', (t) async {
    await pump(t, (o) async => Reply(200, {'state': 'verified'}));
    expect(find.text('You are ID verified'), findsOneWidget);
    expect(find.byKey(const Key('start_id')), findsNothing);
  });
}
