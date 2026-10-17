import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:connectme/features/chat/data/chat_models.dart';
import 'package:connectme/features/chat/data/chat_security.dart';
import 'package:connectme/features/chat/voice/voice_bubble.dart';
import 'package:connectme/features/chat/voice/voice_composer.dart';
import 'package:connectme/features/chat/voice/voice_io.dart';
import 'package:connectme/core/models.dart';
import 'package:connectme/core/providers.dart';
import 'package:connectme/features/discovery/widgets/profile_card.dart';
import 'package:connectme/features/profile/widgets/voice_intro_section.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'chat_rig.dart';
import 'helpers.dart';

class FakeRecorder implements VoiceRecorder {
  FakeRecorder({this.allowed = true, this.clip});
  final bool allowed;
  VoiceClip? clip;
  bool started = false, cancelled = false, stopped = false, disposed = false;

  @override
  Future<bool> start() async => started = allowed;
  @override
  Future<VoiceClip?> stop() async {
    stopped = true;
    return clip;
  }

  @override
  Future<void> cancel() async => cancelled = true;
  @override
  Future<void> dispose() async => disposed = true;
}

class FakePlayer implements VoicePlayer {
  final _pos = StreamController<Duration>.broadcast();
  final _done = StreamController<void>.broadcast();
  Uint8List? played;
  String? mime;
  int pauses = 0, resumes = 0;

  @override
  Future<void> play(Uint8List bytes, String m) async {
    played = bytes;
    mime = m;
  }

  @override
  Future<void> pause() async => pauses++;
  @override
  Future<void> resume() async => resumes++;
  @override
  Stream<Duration> get position => _pos.stream;
  @override
  Stream<void> get completed => _done.stream;
  @override
  Future<void> dispose() async {
    await _pos.close();
    await _done.close();
  }

  void at(int seconds) => _pos.add(Duration(seconds: seconds));
  void finish() => _done.add(null);
}

Widget host(Widget child, {VoiceRecorder? rec, VoicePlayer? player}) => ProviderScope(
      overrides: [
        if (rec != null) voiceRecorderFactoryProvider.overrideWithValue(() => rec),
        if (player != null) voicePlayerFactoryProvider.overrideWithValue(() => player),
      ],
      child: MaterialApp(home: Scaffold(body: child)),
    );

ChatMessage voiceMsg({String text = '12'}) => ChatMessage(
      id: 'v1',
      fromMe: false,
      kind: MsgKind.voice,
      createdAt: DateTime(2026, 1, 1),
      text: text,
      mediaId: 'media-1',
      mediaKey: 'k',
      mime: 'audio/mp4',
    );

void main() {
  group('VoiceComposer', () {
    testWidgets('records, shows a running clock, and sends the clip', (t) async {
      final clip = VoiceClip(bytes: Uint8List.fromList([1, 2, 3]), mime: 'audio/mp4', seconds: 3);
      final rec = FakeRecorder(clip: clip);
      VoiceClip? sent;
      var closed = 0;
      await t.pumpWidget(host(
        VoiceComposer(onSend: (c) async => sent = c, onClose: () => closed++),
        rec: rec,
      ));
      await t.pump();
      expect(rec.started, true);
      expect(find.text('Recording 0:00'), findsOneWidget);

      await t.pump(const Duration(seconds: 2));
      expect(find.text('Recording 0:02'), findsOneWidget);

      await t.tap(find.byKey(const Key('voice_send')));
      await t.pump();
      expect(sent, same(clip));
      expect(closed, 1);
      expect(rec.stopped, true);
    });

    testWidgets('delete throws the recording away without sending', (t) async {
      final rec = FakeRecorder(clip: VoiceClip(bytes: Uint8List(3), mime: 'audio/mp4', seconds: 3));
      var sent = 0, closed = 0;
      await t.pumpWidget(host(VoiceComposer(onSend: (_) async => sent++, onClose: () => closed++), rec: rec));
      await t.pump();
      await t.tap(find.byKey(const Key('voice_cancel')));
      await t.pump();
      expect(rec.cancelled, true);
      expect(sent, 0);
      expect(closed, 1);
    });

    testWidgets('a denied microphone closes the bar with an explanation', (t) async {
      var closed = 0;
      await t.pumpWidget(host(VoiceComposer(onSend: (_) async {}, onClose: () => closed++), rec: FakeRecorder(allowed: false)));
      await t.pump();
      await t.pump(const Duration(milliseconds: 100));
      expect(find.text('Allow the microphone to send voice notes.'), findsOneWidget);
      expect(closed, 1);
      await t.pump(const Duration(seconds: 5));
    });

    testWidgets('stops and sends by itself at the two-minute limit', (t) async {
      final clip = VoiceClip(bytes: Uint8List(3), mime: 'audio/mp4', seconds: maxVoiceSeconds);
      final rec = FakeRecorder(clip: clip);
      VoiceClip? sent;
      await t.pumpWidget(host(VoiceComposer(onSend: (c) async => sent = c, onClose: () {}), rec: rec));
      await t.pump();
      for (var i = 0; i < maxVoiceSeconds; i++) {
        await t.pump(const Duration(seconds: 1));
      }
      expect(sent, same(clip));
      await t.pump(const Duration(seconds: 5));
    });
  });

  group('VoiceBubbleBody', () {
    testWidgets('plays decrypted audio, pauses, resumes, and resets when finished', (t) async {
      final player = FakePlayer();
      final loaded = <String>[];
      await t.pumpWidget(host(
        VoiceBubbleBody(
          message: voiceMsg(),
          color: Colors.black,
          load: (m) async {
            loaded.add(m.mediaId!);
            return Uint8List.fromList([9, 9, 9]);
          },
        ),
        player: player,
      ));
      expect(find.text('0:12'), findsOneWidget, reason: 'shows the duration before playing');
      expect(loaded, isEmpty, reason: 'nothing is downloaded until play');

      await t.tap(find.byKey(const Key('voice_play')));
      await t.pump();
      await t.pump();
      expect(loaded, ['media-1']);
      expect(player.played, Uint8List.fromList([9, 9, 9]));
      expect(player.mime, 'audio/mp4');

      player.at(5);
      await t.pump();
      expect(find.text('0:05'), findsOneWidget);

      await t.tap(find.byKey(const Key('voice_play'))); // pause
      await t.pump();
      expect(player.pauses, 1);
      await t.tap(find.byKey(const Key('voice_play'))); // resume
      await t.pump();
      expect(player.resumes, 1);

      player.finish();
      await t.pump();
      expect(find.text('0:12'), findsOneWidget, reason: 'back to the duration when it ends');
      await t.pumpWidget(const SizedBox());
    });

    testWidgets('a download or decrypt failure says it could not play', (t) async {
      await t.pumpWidget(host(
        VoiceBubbleBody(message: voiceMsg(), color: Colors.black, load: (m) async => throw StateError('gone')),
        player: FakePlayer(),
      ));
      await t.tap(find.byKey(const Key('voice_play')));
      await t.pump();
      await t.pump();
      expect(find.text('Could not play'), findsOneWidget);
      await t.pumpWidget(const SizedBox());
    });
  });

  test('a voice note is encrypted end to end and carries its duration inside the ciphertext', () async {
    final rig = await Rig.create();
    addTearDown(rig.container.dispose);
    await rig.load();

    final audio = Uint8List.fromList(List.generate(3000, (i) => (i * 13) % 251));
    await rig.ctrl.sendVoice(VoiceClip(bytes: audio, mime: 'audio/mp4', seconds: 17));

    final blob = rig.server.blobs.values.single;
    expect(blob.length, greaterThan(audio.length));
    var leaked = false;
    final needle = audio.sublist(0, 64);
    for (var i = 0; i + needle.length <= blob.length && !leaked; i++) {
      leaked = Iterable<int>.generate(needle.length).every((j) => blob[i + j] == needle[j]);
    }
    expect(leaked, isFalse, reason: 'plaintext audio must not be in the upload');

    final msg = rig.server.stored.single;
    expect(msg.type, 'voice');
    final plain = await rig.bob.decryptDirect(conv, Uint8List.fromList(base64.decode(msg.ciphertext)));
    final p = MessagePayload.decode(plain)!;
    expect((p.type, p.text, p.mime), ('voice', '17', 'audio/mp4'));
    expect(await ChatSecurity.decryptAttachment(blob, p.key!), audio);

    final mine = rig.state.messages.single;
    expect(mine.kind, MsgKind.voice);
    expect(mine.text, '17');
  });

  group('voice intro', () {
    OwnProfile me({int secs = 0}) => OwnProfile(ref: 'my-ref', mode: 'open', voiceIntroS: secs);

    Future<FakeAdapter> pumpSection(WidgetTester t, OwnProfile profile, Handler h, {VoiceRecorder? rec, VoicePlayer? player}) async {
      final client = fakeClient(h);
      await t.pumpWidget(ProviderScope(
        overrides: [
          apiClientProvider.overrideWithValue(client),
          if (rec != null) voiceRecorderFactoryProvider.overrideWithValue(() => rec),
          if (player != null) voicePlayerFactoryProvider.overrideWithValue(() => player),
        ],
        child: MaterialApp(home: Scaffold(body: SingleChildScrollView(child: VoiceIntroSection(profile: profile)))),
      ));
      return client.dio.httpClientAdapter as FakeAdapter;
    }

    testWidgets('recording uploads the clip with its length', (t) async {
      final clip = VoiceClip(bytes: Uint8List.fromList([1, 2, 3, 4]), mime: 'audio/mp4', seconds: 9);
      final adapter = await pumpSection(t, me(), (o) async {
        if (o.path == '/users/me/voice-intro') return Reply(204);
        return Reply(404, {'error': 'nope'});
      }, rec: FakeRecorder(clip: clip));
      expect(find.text('Record a voice intro'), findsOneWidget);
      await t.tap(find.byKey(const Key('record_voice_intro')));
      await t.pump();
      expect(find.byKey(const Key('voice_composer')), findsOneWidget);
      await t.pump(const Duration(seconds: 1));
      await t.tap(find.byKey(const Key('voice_send')));
      await t.pump(const Duration(milliseconds: 100));
      final put = adapter.requests.firstWhere((r) => r.path == '/users/me/voice-intro');
      expect(put.method, 'PUT');
      expect(put.queryParameters['seconds'], 9);
      await t.pump(const Duration(seconds: 5));
      await t.pumpWidget(const SizedBox());
    });

    testWidgets('an existing intro can be played from their own address and removed', (t) async {
      final player = FakePlayer();
      final adapter = await pumpSection(t, me(secs: 14), (o) async {
        if (o.path == '/media/voice/my-ref') return Reply(200, [7, 7, 7]);
        if (o.path == '/users/me/voice-intro' && o.method == 'DELETE') return Reply(204);
        return Reply(404, {'error': 'nope'});
      }, player: player);
      expect(find.text('0:14'), findsOneWidget);
      await t.tap(find.byKey(const Key('delete_voice_intro')));
      await t.pump(const Duration(milliseconds: 100));
      expect(adapter.requests.any((r) => r.method == 'DELETE' && r.path == '/users/me/voice-intro'), true);
      await t.pumpWidget(const SizedBox());
    });

    testWidgets('a card offers the voice intro only for a visible (non-anonymous) person', (t) async {
      Widget card(PublicProfile p) => ProviderScope(
            overrides: [apiClientProvider.overrideWithValue(fakeClient((o) async => Reply(404, {'error': 'x'})))],
            child: MaterialApp(home: Scaffold(body: SingleChildScrollView(child: ProfileCard(profile: p)))),
          );
      await t.pumpWidget(card(PublicProfile(ref: 'r', anonymous: false, mode: 'open', displayName: 'Ana', voiceIntroS: 12)));
      expect(find.text('Voice intro'), findsOneWidget);
      await t.pumpWidget(card(PublicProfile(ref: 'r', anonymous: true, mode: 'incognito', voiceIntroS: 12)));
      expect(find.text('Voice intro'), findsNothing, reason: 'an anonymous card never offers a voice');
      await t.pumpWidget(card(PublicProfile(ref: 'r', anonymous: false, mode: 'open', displayName: 'Ana')));
      expect(find.text('Voice intro'), findsNothing);
    });
  });
}
