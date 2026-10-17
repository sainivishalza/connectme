import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models/json.dart';
import '../../core/network/api_client.dart';
import '../../core/providers.dart';

class Chapter {
  Chapter({required this.title, required this.body});
  final String title;
  final String body;

  factory Chapter.fromJson(Map<String, dynamic> j) => Chapter(title: jsonString(j['title']), body: jsonString(j['body']));
  Map<String, dynamic> toJson() => {'title': title, 'body': body};
}

class ChaptersRepository {
  ChaptersRepository(this._api);
  final ApiClient _api;

  Future<List<Chapter>> mine() async => listOf((await _api.getMap('/chapters/me'))['chapters'], Chapter.fromJson);

  Future<List<Chapter>> save(List<Chapter> chapters) async =>
      listOf((await _api.putMap('/chapters/me', body: {'chapters': [for (final c in chapters) c.toJson()]}))['chapters'], Chapter.fromJson);

  /// Someone's chapters, or an empty list when you may not see them.
  Future<List<Chapter>> of(String ref) async {
    try {
      return listOf((await _api.getMap('/chapters/$ref'))['chapters'], Chapter.fromJson);
    } on ApiException catch (e) {
      if (e.status == 404) return [];
      rethrow;
    }
  }
}

final chaptersRepositoryProvider = Provider<ChaptersRepository>((ref) => ChaptersRepository(ref.watch(apiClientProvider)));
