import 'dart:typed_data';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:image_picker/image_picker.dart';

import '../../core/models/json.dart';
import '../../core/network/api_client.dart';
import '../../core/providers.dart';

class VideoClip {
  VideoClip({
    required this.id,
    required this.kind,
    required this.caption,
    required this.seconds,
    required this.createdAt,
    this.likes = 0,
    this.liked = false,
    this.mine = false,
    this.authorRef = '',
    this.authorName = '',
    this.status = 'live',
  });
  final String id;
  final String kind; // short | reel
  final String caption;
  final int seconds;
  final DateTime createdAt;
  final int likes;
  final bool liked;
  final bool mine;
  final String authorRef;
  final String authorName;
  final String status; // live | held (waiting for a moderator)

  bool get held => status == 'held';

  factory VideoClip.fromJson(Map<String, dynamic> j) {
    final a = j['author'] is Map ? Map<String, dynamic>.from(j['author'] as Map) : const <String, dynamic>{};
    return VideoClip(
      id: jsonString(j['id']),
      kind: jsonString(j['kind']),
      caption: jsonString(j['caption']),
      seconds: jsonInt(j['seconds']),
      createdAt: DateTime.tryParse(jsonString(j['created_at'])) ?? DateTime.now(),
      likes: jsonInt(j['likes']),
      liked: jsonBool(j['liked']),
      mine: jsonBool(j['mine']),
      authorRef: jsonString(a['ref']),
      authorName: jsonString(a['name']),
      status: jsonString(j['status']).isEmpty ? 'live' : jsonString(j['status']),
    );
  }

  VideoClip copyWith({int? likes, bool? liked}) => VideoClip(
        id: id,
        kind: kind,
        caption: caption,
        seconds: seconds,
        createdAt: createdAt,
        likes: likes ?? this.likes,
        liked: liked ?? this.liked,
        mine: mine,
        authorRef: authorRef,
        authorName: authorName,
        status: status,
      );
}

class VideosRepository {
  VideosRepository(this._api);
  final ApiClient _api;

  Future<List<VideoClip>> feed({DateTime? before}) async => listOf(
        (await _api.getMap('/videos/feed', query: {'before': ?before?.toUtc().toIso8601String()}))['videos'],
        VideoClip.fromJson,
      );

  Future<List<VideoClip>> mine() async => listOf((await _api.getMap('/videos/mine'))['videos'], VideoClip.fromJson);

  /// The profile reel, or null when there is none or you may not see it.
  Future<VideoClip?> reel(String ref) async {
    try {
      final m = await _api.getMap('/videos/reel/$ref');
      return m['video'] is Map ? VideoClip.fromJson(Map<String, dynamic>.from(m['video'] as Map)) : null;
    } on ApiException catch (e) {
      if (e.status == 404) return null;
      rethrow;
    }
  }

  /// Absolute URL a player can fetch without custom headers (signed, short-lived).
  Future<String> playUrl(String id) async {
    final path = (await _api.getMap('/videos/$id/ticket'))['url'] as String;
    final base = _api.dio.options.baseUrl;
    return Uri.base.resolve(base.endsWith('/') ? '${base.substring(0, base.length - 1)}$path' : '$base$path').toString();
  }

  Future<VideoClip> upload(String kind, Uint8List bytes, {String caption = ''}) async {
    final path = Uri(path: '/videos/', queryParameters: {'kind': kind, if (caption.isNotEmpty) 'caption': caption}).toString();
    final m = await _api.postBytes(path, bytes);
    return VideoClip.fromJson(Map<String, dynamic>.from(m['video'] as Map));
  }

  Future<({bool liked, int likes})> like(String id) async {
    final m = await _api.postMap('/videos/$id/like');
    return (liked: jsonBool(m['liked']), likes: jsonInt(m['likes']));
  }

  Future<void> report(String id, String reason) => _api.post('/videos/$id/report', body: {'reason': reason});
  Future<void> delete(String id) => _api.delete('/videos/$id');
}

final videosRepositoryProvider = Provider<VideosRepository>((ref) => VideosRepository(ref.watch(apiClientProvider)));

/// A profile's reel as the signed-in person may see it (null when none).
final reelProvider = FutureProvider.autoDispose.family<VideoClip?, String>((ref, profileRef) => ref.watch(videosRepositoryProvider).reel(profileRef));

/// Picks a clip from the gallery or camera. Tests replace this.
class PickedVideo {
  PickedVideo(this.bytes);
  final Uint8List bytes;
}

typedef VideoPicker = Future<PickedVideo?> Function({required bool camera});

final videoPickerProvider = Provider<VideoPicker>((ref) => ({required bool camera}) async {
      final f = await ImagePicker().pickVideo(
        source: camera ? ImageSource.camera : ImageSource.gallery,
        maxDuration: const Duration(seconds: 30),
      );
      return f == null ? null : PickedVideo(await f.readAsBytes());
    });
