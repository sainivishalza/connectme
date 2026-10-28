import 'package:connectme/core/models.dart';
import 'package:connectme/core/providers.dart';
import 'package:connectme/features/chat/data/chat_models.dart';
import 'package:connectme/features/chat/widgets/voice_step_banner.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

Conversation conv(VoiceStepInfo? v) =>
    Conversation(id: 'm1', title: 'Anonymous', subtitle: '', isGroup: false, anonymous: true, voiceStep: v);

Future<FakeAdapter> pump(WidgetTester t, Conversation c) async {
  final client = fakeClient((o) async => Reply(204));
  await t.pumpWidget(ProviderScope(
    overrides: [apiClientProvider.overrideWithValue(client)],
    child: MaterialApp(home: Scaffold(body: VoiceStepBanner(conv: c))),
  ));
  await t.pumpAndSettle();
  return client.dio.httpClientAdapter as FakeAdapter;
}

void main() {
  testWidgets('offers to swap voices, then waits, then plays once both agreed', (t) async {
    final a = await pump(t, conv(const VoiceStepInfo()));
    expect(find.byKey(const Key('voice_step_ask')), findsOneWidget);
    await t.tap(find.byKey(const Key('voice_step_ask')));
    await t.pumpAndSettle();
    expect(a.requests.single.path, '/matching/matches/m1/reveal-voice');

    await pump(t, conv(const VoiceStepInfo(byMe: true)));
    expect(find.byKey(const Key('voice_waiting')), findsOneWidget);
    expect(find.byKey(const Key('voice_step_ask')), findsNothing);

    await pump(t, conv(const VoiceStepInfo(byMe: true, unlocked: true, seconds: 12)));
    expect(find.byKey(const Key('match_voice')), findsOneWidget);
  });

  testWidgets('shows nothing when the server sent no voice step', (t) async {
    await pump(t, conv(null));
    expect(find.byKey(const Key('voice_step')), findsNothing);
  });
}
