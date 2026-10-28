import 'package:connectme/core/providers.dart';
import 'package:connectme/core/router/app_router.dart';
import 'package:connectme/features/auth/auth_controller.dart';
import 'package:connectme/features/investors/invest_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

class _SignedIn extends AuthController {
  @override
  AuthState build() => const AuthState(AuthStatus.signedIn, userId: 'u1');
}

class _SignedOut extends AuthController {
  @override
  AuthState build() => const AuthState(AuthStatus.signedOut);
}

Map<String, dynamic> board() => {
      'pool_percent': '49',
      'holders': [
        {'rank': 1, 'name': 'Vishal Saini', 'percent': '51', 'founder': true},
        {'rank': 2, 'name': 'Early Investor', 'percent': '24.5'},
        {'rank': 3, 'name': 'Anonymous investor', 'percent': '0.001'},
      ],
    };

Future<FakeAdapter> pump(WidgetTester t, bool signedIn, Handler h) async {
  t.view.physicalSize = const Size(800, 2400);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.reset);
  final client = fakeClient(h);
  await t.pumpWidget(ProviderScope(
    overrides: [
      apiClientProvider.overrideWithValue(client),
      authControllerProvider.overrideWith(signedIn ? _SignedIn.new : _SignedOut.new),
    ],
    child: const MaterialApp(home: InvestScreen()),
  ));
  await t.pumpAndSettle();
  return client.dio.httpClientAdapter as FakeAdapter;
}

void main() {
  investFixedTests();
  test('the invest page is open to people who are signed out', () {
    expect(redirectFor(AuthStatus.signedOut, '/invest'), isNull);
    expect(redirectFor(AuthStatus.signedIn, '/invest'), isNull);
    expect(redirectFor(AuthStatus.unknown, '/invest'), isNull);
    expect(redirectFor(AuthStatus.unknown, '/safety/confirm'), isNull);
    expect(redirectFor(AuthStatus.unknown, '/discover'), '/');
  });

  testWidgets('the board shows the founder first with exact decimals, and signed-out visitors are asked to sign in', (t) async {
    await pump(t, false, (o) async => o.path == '/investors/public' ? Reply(200, board()) : Reply(404));
    expect(find.text('Vishal Saini'), findsOneWidget);
    expect(find.byKey(const Key('holder_pct_1')), findsOneWidget);
    expect(find.text('51%'), findsOneWidget);
    expect(find.text('24.5%'), findsOneWidget);
    expect(find.text('0.001%'), findsOneWidget);
    expect(find.byKey(const Key('invest_notice')), findsOneWidget);
    expect(find.byKey(const Key('invest_signin')), findsOneWidget);
    expect(find.byKey(const Key('invest_amount')), findsNothing);
  });

  testWidgets('a member must enter a valid amount and accept the notice before pledging', (t) async {
    final pledges = <Map<String, dynamic>>[];
    final a = await pump(t, true, (o) async {
      if (o.path == '/investors/public') return Reply(200, board());
      if (o.path == '/investors/mine') {
        return Reply(200, {'pledges': pledges, 'percent': '0', 'payment_note': 'Bank transfer details are sent by the owner.'});
      }
      if (o.path == '/investors/pledges' && o.method == 'POST') {
        final p = {'id': 'p1', 'amount_usd': (o.data as Map)['amount_usd'], 'status': 'pending', 'reference': 'INV-ABC12345'};
        pledges.add(p);
        return Reply(201, p);
      }
      if (o.path == '/investors/pledges/p1' && o.method == 'DELETE') {
        pledges.clear();
        return Reply(204);
      }
      return Reply(404);
    });
    FilledButton submit() => t.widget<FilledButton>(find.byKey(const Key('invest_submit')));
    expect(submit().onPressed, isNull);
    await t.enterText(find.byKey(const Key('invest_amount')), '0.5');
    await t.tap(find.byKey(const Key('invest_accept')));
    await t.pump();
    expect(submit().onPressed, isNull, reason: 'under one dollar');
    await t.enterText(find.byKey(const Key('invest_amount')), '25.50');
    await t.pump();
    expect(submit().onPressed, isNotNull);
    await t.tap(find.byKey(const Key('invest_submit')));
    await t.pumpAndSettle();
    final sent = a.requests.firstWhere((r) => r.path == '/investors/pledges' && r.method == 'POST').data as Map;
    expect(sent, {'amount_usd': '25.50', 'show_name': true, 'accept_terms': true});
    expect(find.text('\$25.50'), findsOneWidget);
    expect(find.textContaining('INV-ABC12345'), findsOneWidget);
    expect(find.byKey(const Key('payment_note')), findsOneWidget);

    await t.tap(find.byKey(const Key('cancel_p1')));
    await t.pumpAndSettle();
    expect(find.text('\$25.50'), findsNothing);
  });
}

void investFixedTests() {
  testWidgets('the fixed-price model says so, and explains that the pool sells out instead of diluting', (t) async {
    await pump(t, true, (o) async {
      if (o.path == '/investors/public') {
        return Reply(200, {
          ...board(),
          'model': 'fixed',
          'price_per_percent': '500',
          'open_percent': '47',
        });
      }
      return Reply(200, {'pledges': [], 'percent': '0'});
    });
    final notice = t.widget<Text>(find.descendant(of: find.byKey(const Key('invest_notice')), matching: find.byType(Text))).data!;
    expect(notice, contains('fixed price of \$500 for 1%'));
    expect(notice, isNot(contains('lowers everyone')));
    expect(find.textContaining('0.002% at the fixed price'), findsOneWidget);
  });
}
