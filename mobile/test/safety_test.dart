import 'package:connectme/core/providers.dart';
import 'package:connectme/features/chat/data/safety_scan.dart';
import 'package:connectme/features/chat/widgets/risk_banner.dart';
import 'package:connectme/features/safety/safety_screen.dart';
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
  group('scam warning scanner', () {
    test('flags requests for money, other apps and private details', () {
      expect(SafetyScan.check('can you buy me a gift card? i will pay you back')?.kind, RiskKind.money);
      expect(SafetyScan.check('I need some money for my medical bills')?.kind, RiskKind.money);
      expect(SafetyScan.check('Have you tried bitcoin? great investment platform')?.kind, RiskKind.money);
      expect(SafetyScan.check('add me on WhatsApp, this app is slow')?.kind, RiskKind.offPlatform);
      expect(SafetyScan.check('look at this https://example.com/prize')?.kind, RiskKind.offPlatform);
      expect(SafetyScan.check('send me a photo of your passport please')?.kind, RiskKind.personalDetails);
      expect(SafetyScan.check('what is your home address?')?.kind, RiskKind.personalDetails);
      expect(SafetyScan.check("what's your address?")?.kind, RiskKind.personalDetails);
    });

    test('also reads Spanish and Hindi, spelled either way', () {
      expect(SafetyScan.check('necesito dinero urgente, envíame plata')?.kind, RiskKind.money);
      expect(SafetyScan.check('mujhe paise chahiye, udhaar de do')?.kind, RiskKind.money);
      expect(SafetyScan.check('मुझे पैसे भेज दो')?.kind, RiskKind.money);
      expect(SafetyScan.check('hablemos por whatsapp')?.kind, RiskKind.offPlatform);
      expect(SafetyScan.check('apna aadhaar bhejo')?.kind, RiskKind.personalDetails);
      expect(SafetyScan.check('मुझे आपका आधार चाहिए')?.kind, RiskKind.personalDetails);
      for (final ok in ['me encanta el jazz', 'kal milte hain', 'आज मौसम अच्छा है', 'cenamos el viernes?']) {
        expect(SafetyScan.check(ok), isNull, reason: ok);
      }
    });

    test('stays quiet on ordinary conversation', () {
      for (final ok in ['hey, how was your weekend?', 'I love jazz and long walks', 'dinner on Friday at 8?', 'haha that is so funny', 'I am saving for a trip', 'ok', '']) {
        expect(SafetyScan.check(ok), isNull, reason: ok);
      }
    });

    test('money outranks a link in the same message', () {
      expect(SafetyScan.check('send money here https://pay.example')?.kind, RiskKind.money);
    });
  });

  testWidgets('the warning banner offers report, block and dismiss', (t) async {
    final seen = <String>[];
    await t.pumpWidget(MaterialApp(
      home: Scaffold(
        body: RiskBanner(
          risk: SafetyScan.check('send me a gift card')!,
          onDismiss: () => seen.add('dismiss'),
          onReport: () => seen.add('report'),
          onBlock: () => seen.add('block'),
        ),
      ),
    ));
    expect(find.text('This message may be asking for money'), findsOneWidget);
    await t.tap(find.byKey(const Key('risk_report')));
    await t.tap(find.byKey(const Key('risk_block')));
    await t.tap(find.byKey(const Key('risk_dismiss')));
    expect(seen, ['report', 'block', 'dismiss']);
  });

  testWidgets('safety center: tips, contacts with their status, and a plan that can be checked in', (t) async {
    t.view.physicalSize = const Size(800, 2400);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.reset);
    final contacts = <Map<String, dynamic>>[
      {'id': 'c1', 'name': 'Aunt Rosa', 'email': 'rosa@example.org', 'confirmed': true},
      {'id': 'c2', 'name': 'Brother', 'email': 'bro@example.org', 'confirmed': false},
    ];
    var plans = <Map<String, dynamic>>[
      {
        'id': 'p1', 'contact': 'Aunt Rosa', 'place': 'Cafe Luna', 'status': 'alerted', 'can_extend': false, 'who': 'Mina',
        'meet_at': DateTime.now().toUtc().toIso8601String(), 'checkin_by': DateTime.now().toUtc().toIso8601String(),
      }
    ];
    final posted = <String>[];
    final client = fakeClient((o) async {
      if (o.path == '/safety/contacts' && o.method == 'GET') return Reply(200, {'contacts': contacts});
      if (o.path == '/safety/plans' && o.method == 'GET') return Reply(200, {'plans': plans});
      if (o.path == '/safety/plans/p1/safe') {
        posted.add('safe');
        plans = [];
        return Reply(204);
      }
      if (o.path == '/safety/contacts' && o.method == 'POST') {
        posted.add('add ${(o.data as Map)['email']}');
        return Reply(201, {'id': 'c3'});
      }
      return Reply(404);
    });
    await t.pumpWidget(ProviderScope(overrides: [apiClientProvider.overrideWithValue(client)], child: const MaterialApp(home: SafetyScreen())));
    await settle(t);

    expect(find.text('Before you meet'), findsOneWidget);
    expect(find.textContaining('agreed'), findsOneWidget);
    expect(find.textContaining('waiting for them to agree'), findsOneWidget);
    // An overdue plan says so and offers to tell the contact all is well.
    expect(find.byKey(const Key('alerted_note')), findsOneWidget);
    await t.tap(find.byKey(const Key('safe_p1')));
    await settle(t);
    expect(posted, ['safe']);
    expect(find.byKey(const Key('plan_p1')), findsNothing);

    await t.tap(find.byKey(const Key('add_contact')));
    await t.pumpAndSettle();
    await t.enterText(find.byKey(const Key('contact_name')), 'Friend');
    await t.enterText(find.byKey(const Key('contact_email')), 'friend@example.org');
    await t.tap(find.byKey(const Key('contact_save')));
    await settle(t);
    expect(posted.last, 'add friend@example.org');
  });

  testWidgets('a contact can get text alerts: add a number, enter the code they read out, then texts are on', (t) async {
    t.view.physicalSize = const Size(800, 2400);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.reset);
    var phone = '';
    var confirmed = false;
    final calls = <String>[];
    final client = fakeClient((o) async {
      if (o.path == '/safety/contacts' && o.method == 'GET') {
        return Reply(200, {
          'contacts': [
            {'id': 'c1', 'name': 'Aunt Rosa', 'email': 'r@example.org', 'confirmed': true, 'phone': phone, 'phone_confirmed': confirmed},
            {'id': 'c2', 'name': 'Brother', 'email': 'b@example.org', 'confirmed': false},
          ]
        });
      }
      if (o.path == '/safety/plans') return Reply(200, {'plans': []});
      if (o.path == '/safety/contacts/c1/phone' && o.method == 'PUT') {
        calls.add('set ${(o.data as Map)['phone']}');
        phone = '+*********23';
        return Reply(204);
      }
      if (o.path == '/safety/contacts/c1/phone/confirm') {
        calls.add('code ${(o.data as Map)['code']}');
        confirmed = true;
        return Reply(204);
      }
      return Reply(404);
    });
    await t.pumpWidget(ProviderScope(overrides: [apiClientProvider.overrideWithValue(client)], child: const MaterialApp(home: SafetyScreen())));
    await settle(t);
    // Only a contact who has agreed by email can be given a number.
    expect(find.byKey(const Key('add_phone_c1')), findsOneWidget);
    expect(find.byKey(const Key('add_phone_c2')), findsNothing);

    await t.ensureVisible(find.byKey(const Key('add_phone_c1')));
    await t.tap(find.byKey(const Key('add_phone_c1')));
    await t.pumpAndSettle();
    await t.enterText(find.byKey(const Key('phone_field')), '+14155550123');
    await t.tap(find.byKey(const Key('phone_ok')));
    await settle(t);
    expect(calls, ['set +14155550123']);
    expect(find.textContaining('Code sent to +*********23'), findsOneWidget);
    expect(find.byKey(const Key('texts_on_c1')), findsNothing);

    await t.tap(find.byKey(const Key('enter_code_c1')));
    await t.pumpAndSettle();
    await t.enterText(find.byKey(const Key('code_field')), '123456');
    await t.tap(find.byKey(const Key('phone_ok')));
    await settle(t);
    expect(calls.last, 'code 123456');
    expect(find.byKey(const Key('texts_on_c1')), findsOneWidget);
  });

  testWidgets('a plan needs a place and sends the match, the check-in time and the name choice', (t) async {
    t.view.physicalSize = const Size(800, 2400);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.reset);
    Object? sent;
    final client = fakeClient((o) async {
      if (o.path == '/safety/contacts') {
        return Reply(200, {
          'contacts': [
            {'id': 'c1', 'name': 'Aunt Rosa', 'email': 'r@example.org', 'confirmed': true}
          ]
        });
      }
      if (o.path == '/safety/plans' && o.method == 'GET') return Reply(200, {'plans': []});
      if (o.path == '/safety/plans' && o.method == 'POST') {
        sent = o.data;
        return Reply(201, {'id': 'p9'});
      }
      return Reply(404);
    });
    await t.pumpWidget(ProviderScope(overrides: [apiClientProvider.overrideWithValue(client)], child: const MaterialApp(home: SafetyScreen(matchId: 'm1'))));
    await settle(t);
    await t.tap(find.byKey(const Key('new_plan')));
    await t.pumpAndSettle();
    expect(t.widget<FilledButton>(find.byKey(const Key('plan_send'))).onPressed, isNull, reason: 'needs a place');
    await t.enterText(find.byKey(const Key('plan_place')), 'Cafe Luna, Main St');
    await t.tap(find.byKey(const Key('after_240')));
    await t.tap(find.byKey(const Key('plan_share_name')));
    await t.pump();
    await t.tap(find.byKey(const Key('plan_send')));
    await settle(t);
    final body = sent! as Map;
    expect(body['contact_id'], 'c1');
    expect(body['place'], 'Cafe Luna, Main St');
    expect(body['checkin_after_min'], 240);
    expect(body['match_id'], 'm1');
    expect(body['share_name'], true);
  });

  testWidgets('the contact opens the emailed link and agrees', (t) async {
    final posted = <Object?>[];
    final client = fakeClient((o) async {
      if (o.path == '/safety/confirm') {
        posted.add(o.data);
        return Reply(204);
      }
      return Reply(404);
    });
    await t.pumpWidget(ProviderScope(overrides: [apiClientProvider.overrideWithValue(client)], child: const MaterialApp(home: ContactConfirmScreen(token: 'abc123'))));
    await t.pumpAndSettle();
    await t.tap(find.byKey(const Key('confirm_agree')));
    await t.pumpAndSettle();
    expect(posted, [
      {'token': 'abc123'}
    ]);
    expect(find.byKey(const Key('confirm_done')), findsOneWidget);
  });
}
