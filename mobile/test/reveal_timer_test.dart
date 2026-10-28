import 'package:connectme/core/models.dart';
import 'package:connectme/core/providers.dart';
import 'package:connectme/features/chat/data/chat_models.dart';
import 'package:connectme/features/chat/widgets/reveal_timer_banner.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

Conversation conv(RevealTimerInfo? rt) => Conversation(id: 'm1', title: 'Anonymous', subtitle: '', isGroup: false, anonymous: true, revealTimer: rt);

Future<FakeAdapter> pump(WidgetTester t, Conversation c) async {
  final client = fakeClient((o) async => Reply(204));
  await t.pumpWidget(ProviderScope(
    overrides: [apiClientProvider.overrideWithValue(client)],
    child: MaterialApp(home: Scaffold(body: RevealTimerBanner(conv: c))),
  ));
  await t.pumpAndSettle();
  return client.dio.httpClientAdapter as FakeAdapter;
}

void main() {
  final at = DateTime.now().add(const Duration(hours: 3));

  testWidgets('offers a choice of times and proposes the one picked', (t) async {
    final a = await pump(t, conv(null));
    await t.tap(find.byKey(const Key('reveal_timer_ask')));
    await t.pumpAndSettle();
    await t.tap(find.text('In 24 hours'));
    await t.pumpAndSettle();
    expect(a.requests.single.path, '/matching/matches/m1/reveal-timer');
    expect(a.requests.single.data, {'hours': 24});
  });

  testWidgets('shows what the other person suggested and lets you agree or decline', (t) async {
    final a = await pump(t, conv(RevealTimerInfo(at: at, needsReply: true)));
    expect(find.byKey(const Key('reveal_timer_offer')), findsOneWidget);
    await t.tap(find.byKey(const Key('reveal_timer_no')));
    await t.pumpAndSettle();
    expect(a.requests.last.method, 'DELETE');
    await t.tap(find.byKey(const Key('reveal_timer_yes')));
    await t.pumpAndSettle();
    expect(a.requests.last.method, 'POST');
  });

  testWidgets('waiting and agreed states both offer to cancel', (t) async {
    await pump(t, conv(RevealTimerInfo(at: at, byMe: true)));
    expect(find.byKey(const Key('reveal_timer_waiting')), findsOneWidget);
    expect(find.byKey(const Key('reveal_timer_cancel')), findsOneWidget);
    await pump(t, conv(RevealTimerInfo(at: at, accepted: true)));
    expect(find.byKey(const Key('reveal_timer_set')), findsOneWidget);
    expect(find.byKey(const Key('reveal_timer_cancel')), findsOneWidget);
  });
}
