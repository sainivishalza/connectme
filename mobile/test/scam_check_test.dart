import 'package:connectme/core/providers.dart';
import 'package:connectme/features/chat/data/safety_scan.dart';
import 'package:connectme/features/chat/data/scam_check.dart';
import 'package:connectme/features/chat/widgets/risk_banner.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

void main() {
  testWidgets('the banner offers "Look closer" only when a check is available', (t) async {
    var asked = 0;
    Widget banner({VoidCallback? ai}) => MaterialApp(
      home: Scaffold(
        body: RiskBanner(risk: SafetyScan.check('send me a gift card')!, onDismiss: () {}, onReport: () {}, onBlock: () {}, onAiCheck: ai),
      ),
    );
    await t.pumpWidget(banner());
    expect(find.byKey(const Key('risk_ai')), findsNothing);
    await t.pumpWidget(banner(ai: () => asked++));
    await t.tap(find.byKey(const Key('risk_ai')));
    expect(asked, 1);
  });

  test('only the shared lines are sent, as me or other, and the answer is read back', () async {
    final client = fakeClient((o) async => Reply(200, {
      'risk': 'high',
      'score': 80,
      'advice': 'Please stop.',
      'signals': [{'code': 'money', 'text': 'They asked for money'}],
    }));
    final container = ProviderContainer(overrides: [apiClientProvider.overrideWithValue(client)]);
    addTearDown(container.dispose);
    final r = await container.read(scamCheckRepositoryProvider).check('m1', [(fromMe: false, text: 'send money'), (fromMe: true, text: 'no')]);
    final sent = (client.dio.httpClientAdapter as FakeAdapter).requests.single;
    expect(sent.path, '/scamcheck/m1');
    expect(sent.data, {
      'lines': [
        {'who': 'other', 'text': 'send money'},
        {'who': 'me', 'text': 'no'},
      ],
    });
    expect(r.risk, 'high');
    expect(r.signals.single.text, 'They asked for money');
  });
}
