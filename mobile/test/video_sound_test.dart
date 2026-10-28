import 'dart:typed_data';

import 'package:connectme/core/providers.dart';
import 'package:connectme/features/videos/screens/video_feed_screen.dart';
import 'package:connectme/features/videos/sounds.dart';
import 'package:connectme/features/videos/videos_repository.dart';
import 'package:connectme/features/videos/widgets/video_view.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

class FakeLoop implements SoundLoop {
  final events = <String>[];
  @override
  Future<void> play(Uint8List wav) async => events.add('play ${wav.length}');
  @override
  Future<void> stop() async => events.add('stop');
}

Map<String, dynamic> clip(String id, {String sound = ''}) => {
      'id': id, 'kind': 'short', 'caption': 'A clip', 'seconds': 8, 'likes': 0, 'mine': false, 'comments': 0,
      'created_at': DateTime.now().subtract(const Duration(minutes: 3)).toUtc().toIso8601String(),
      'author': {'ref': 'a', 'name': 'Dana'},
      if (sound.isNotEmpty) ...{'sound': sound, 'sound_name': 'Sunrise'},
    };

void main() {
  testWidgets('a clip with a sound shows its name, plays the loop, and the toggle mutes it', (t) async {
    final loop = FakeLoop();
    final client = fakeClient((o) async {
      if (o.path == '/videos/feed') return Reply(200, {'videos': [clip('v1', sound: 'sunrise'), clip('v2')]});
      return Reply(404);
    });
    // The sound file route returns bytes.
    final adapterClient = fakeClient((o) async {
      if (o.path == '/videos/feed') return Reply(200, {'videos': [clip('v1', sound: 'sunrise'), clip('v2')]});
      return Reply(404);
    });
    expect(client, isNotNull);
    await t.pumpWidget(ProviderScope(
      overrides: [
        apiClientProvider.overrideWithValue(adapterClient),
        videoViewBuilderProvider.overrideWithValue((id, {autoplay = true}) => const SizedBox()),
        soundLoopProvider.overrideWithValue(loop),
        soundsRepositoryProvider.overrideWithValue(_FakeSounds()),
      ],
      child: const MaterialApp(home: VideoFeedScreen()),
    ));
    await t.pumpAndSettle();
    expect(find.byKey(const Key('sound_name_v1')), findsOneWidget);
    expect(loop.events, ['play 4']);

    await t.tap(find.byKey(const Key('sound_toggle')));
    await t.pumpAndSettle();
    expect(loop.events.last, 'stop');
    await t.tap(find.byKey(const Key('sound_toggle')));
    await t.pumpAndSettle();
    expect(loop.events.last, 'play 4');

    // Swiping to a clip with no sound stops the loop.
    await t.drag(find.byType(PageView), const Offset(0, -600));
    await t.pumpAndSettle();
    expect(loop.events.last, 'stop');
    expect(find.byKey(const Key('sound_toggle')), findsNothing);
    await t.pumpWidget(const SizedBox());
  });

  test('a clip reads its sound from the server answer', () {
    final c = VideoClip.fromJson(clip('v9', sound: 'pulse'));
    expect((c.sound, c.soundName), ('pulse', 'Sunrise'));
    expect(VideoClip.fromJson(clip('v8')).sound, '');
  });
}

class _FakeSounds implements SoundsRepository {
  @override
  Future<Uint8List> file(String id) async => Uint8List.fromList([1, 2, 3, 4]);
  @override
  Future<List<SoundInfo>> list() async => const [SoundInfo(id: 'sunrise', name: 'Sunrise', mood: 'warm', seconds: 10)];
}
