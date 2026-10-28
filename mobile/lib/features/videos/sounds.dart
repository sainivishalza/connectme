import 'dart:typed_data';

import 'package:audioplayers/audioplayers.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models/json.dart';
import '../../core/network/api_client.dart';
import '../../core/providers.dart';

/// One loop in the built-in library (synthesised on the server, nothing licensed).
class SoundInfo {
  const SoundInfo({required this.id, required this.name, required this.mood, required this.seconds});
  final String id;
  final String name;
  final String mood;
  final double seconds;

  factory SoundInfo.fromJson(Map<String, dynamic> j) =>
      SoundInfo(id: jsonString(j['id']), name: jsonString(j['name']), mood: jsonString(j['mood']), seconds: j['seconds'] is num ? (j['seconds'] as num).toDouble() : 0);
}

class SoundsRepository {
  SoundsRepository(this._api);
  final ApiClient _api;

  Future<List<SoundInfo>> list() async => listOf((await _api.getMap('/sounds/'))['sounds'], SoundInfo.fromJson);
  Future<Uint8List> file(String id) => _api.getBytes('/sounds/$id');
}

final soundsRepositoryProvider = Provider<SoundsRepository>((ref) => SoundsRepository(ref.watch(apiClientProvider)));

final soundLibraryProvider = FutureProvider.autoDispose<List<SoundInfo>>((ref) => ref.watch(soundsRepositoryProvider).list());

/// Plays one loop at a time under a clip. Tests replace this.
abstract class SoundLoop {
  Future<void> play(Uint8List wav);
  Future<void> stop();
}

class DeviceSoundLoop implements SoundLoop {
  // Created on first use, so screens that never play a sound never touch the audio plugin.
  late final AudioPlayer _p = AudioPlayer();

  @override
  Future<void> play(Uint8List wav) async {
    await _p.setReleaseMode(ReleaseMode.loop);
    await _p.setVolume(0.55);
    await _p.play(BytesSource(wav, mimeType: 'audio/wav'));
  }

  @override
  Future<void> stop() => _p.stop();
}

final soundLoopProvider = Provider<SoundLoop>((ref) {
  final loop = DeviceSoundLoop();
  return loop;
});
