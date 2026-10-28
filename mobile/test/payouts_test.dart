import 'package:connectme/core/providers.dart';
import 'package:connectme/features/payouts/payouts.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

Future<FakeAdapter> pump(WidgetTester t, Map<String, dynamic> earnings, {void Function()? onRequest}) async {
  t.view.physicalSize = const Size(800, 1800);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.reset);
  final client = fakeClient((o) async {
    if (o.path == '/payouts/mine') return Reply(200, earnings);
    if (o.path == '/payouts/request') {
      onRequest?.call();
      return Reply(201, {'id': 'p9', 'coins': 6000, 'status': 'requested'});
    }
    return Reply(404);
  });
  await t.pumpWidget(ProviderScope(overrides: [apiClientProvider.overrideWithValue(client)], child: const MaterialApp(home: EarningsScreen())));
  await t.pumpAndSettle();
  return client.dio.httpClientAdapter as FakeAdapter;
}

void main() {
  testWidgets('a verified host with enough coins can ask for a payout', (t) async {
    final a = await pump(t, {'available_coins': 6000, 'requested_coins': 0, 'paid_coins': 0, 'min_payout_coins': 5000, 'can_request': true, 'payouts': []});
    expect(find.text('6000 coins'), findsOneWidget);
    await t.tap(find.byKey(const Key('request_payout')));
    await t.pumpAndSettle();
    expect(a.count('/payouts/request'), 1);
    expect(find.textContaining('Payout requested'), findsOneWidget);
  });

  testWidgets('without a verified ID or below the minimum the button is off and says why', (t) async {
    await pump(t, {'available_coins': 9000, 'can_request': false, 'needs_id': 'Verify your ID to receive payouts.', 'payouts': []});
    expect(t.widget<FilledButton>(find.byKey(const Key('request_payout'))).onPressed, isNull);
    expect(find.byKey(const Key('needs_id')), findsOneWidget);
  });

  testWidgets('a waiting request and the history are shown, paid ones with their reference', (t) async {
    await pump(t, {
      'available_coins': 0,
      'requested_coins': 5600,
      'paid_coins': 7000,
      'can_request': false,
      'payouts': [
        {'id': 'a', 'coins': 5600, 'status': 'requested'},
        {'id': 'b', 'coins': 7000, 'status': 'paid', 'amount_cents': 7000, 'reference': 'UPI-1234'},
        {'id': 'c', 'coins': 5100, 'status': 'rejected', 'note': 'bank details missing'},
      ],
    });
    expect(find.byKey(const Key('requested_note')), findsOneWidget);
    expect(find.textContaining('Paid. Reference UPI-1234'), findsOneWidget);
    expect(find.text('7000 coins - \$70.00'), findsOneWidget);
    expect(find.textContaining('bank details missing'), findsOneWidget);
    expect(find.text('Waiting for the team'), findsOneWidget);
  });
}
