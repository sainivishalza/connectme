import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models/json.dart';
import '../../core/network/api_client.dart';
import '../../core/providers.dart';

class Board {
  Board({required this.slug, required this.title, required this.description, required this.kind, required this.threads});
  final String slug;
  final String title;
  final String description;
  final String kind; // advice | travel | city
  final int threads;

  factory Board.fromJson(Map<String, dynamic> j) => Board(
        slug: jsonString(j['slug']),
        title: jsonString(j['title']),
        description: jsonString(j['description']),
        kind: jsonString(j['kind']),
        threads: jsonInt(j['threads']),
      );
}

class ForumThread {
  ForumThread({
    required this.id,
    required this.title,
    required this.author,
    required this.createdAt,
    required this.lastReplyAt,
    this.body = '',
    this.replies = 0,
    this.mine = false,
  });
  final String id;
  final String title;
  final String body;
  final String author;
  final int replies;
  final bool mine;
  final DateTime createdAt;
  final DateTime lastReplyAt;

  factory ForumThread.fromJson(Map<String, dynamic> j) => ForumThread(
        id: jsonString(j['id']),
        title: jsonString(j['title']),
        body: jsonString(j['body']),
        author: jsonString(j['author']),
        replies: jsonInt(j['replies']),
        mine: jsonBool(j['mine']),
        createdAt: DateTime.tryParse(jsonString(j['created_at'])) ?? DateTime.now(),
        lastReplyAt: DateTime.tryParse(jsonString(j['last_reply_at'])) ?? DateTime.now(),
      );
}

class ForumReply {
  ForumReply({required this.id, required this.body, required this.author, required this.createdAt, this.mine = false});
  final int id;
  final String body;
  final String author;
  final bool mine;
  final DateTime createdAt;

  factory ForumReply.fromJson(Map<String, dynamic> j) => ForumReply(
        id: jsonInt(j['id']),
        body: jsonString(j['body']),
        author: jsonString(j['author']),
        mine: jsonBool(j['mine']),
        createdAt: DateTime.tryParse(jsonString(j['created_at'])) ?? DateTime.now(),
      );
}

class ForumsRepository {
  ForumsRepository(this._api);
  final ApiClient _api;

  Future<List<Board>> boards() async => listOf((await _api.getMap('/forums/boards'))['boards'], Board.fromJson);

  Future<List<ForumThread>> threads(String slug, {DateTime? before}) async => listOf(
        (await _api.getMap('/forums/boards/$slug/threads', query: {'before': ?before?.toUtc().toIso8601String()}))['threads'],
        ForumThread.fromJson,
      );

  Future<ForumThread> createThread(String slug, {required String title, required String body}) async =>
      ForumThread.fromJson(await _api.postMap('/forums/boards/$slug/threads', body: {'title': title, 'body': body}));

  Future<ForumThread> thread(String id) async => ForumThread.fromJson(await _api.getMap('/forums/threads/$id'));

  Future<List<ForumReply>> replies(String id, {int after = 0}) async =>
      listOf((await _api.getMap('/forums/threads/$id/replies', query: {'after': after}))['replies'], ForumReply.fromJson);

  Future<ForumReply> reply(String id, String body) async =>
      ForumReply.fromJson(await _api.postMap('/forums/threads/$id/replies', body: {'body': body}));

  Future<void> deleteThread(String id) => _api.delete('/forums/threads/$id');
  Future<void> deleteReply(int id) => _api.delete('/forums/replies/$id');

  /// [kind] is 'threads' or 'replies'.
  Future<void> report(String kind, Object id, String reason) => _api.post('/forums/$kind/$id/report', body: {'reason': reason});
}

final forumsRepositoryProvider = Provider<ForumsRepository>((ref) => ForumsRepository(ref.watch(apiClientProvider)));

final forumBoardsProvider = FutureProvider.autoDispose<List<Board>>((ref) => ref.watch(forumsRepositoryProvider).boards());
