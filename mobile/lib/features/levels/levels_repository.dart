import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models/json.dart';
import '../../core/network/api_client.dart';
import '../../core/providers.dart';

/// One ladder position. [total] is the caller's own number; it is never sent
/// about anyone else.
class LevelInfo {
  const LevelInfo({this.level = 1, this.title = 'Newcomer', this.total = 0, this.nextAt = 0, this.progress = 0, this.maxed = false});
  final int level;
  final String title;
  final int total;
  final int nextAt;
  final int progress;
  final bool maxed;

  String get label => 'Lv $level $title';

  factory LevelInfo.fromJson(Object? o) {
    final j = o is Map ? Map<String, dynamic>.from(o) : <String, dynamic>{};
    return LevelInfo(
      level: j['level'] is num ? jsonInt(j['level']) : 1,
      title: jsonString(j['title']).isEmpty ? 'Newcomer' : jsonString(j['title']),
      total: jsonInt(j['total']),
      nextAt: jsonInt(j['next_at']),
      progress: jsonInt(j['progress']),
      maxed: jsonBool(j['maxed']),
    );
  }
}

class MyLevels {
  const MyLevels({this.charm = const LevelInfo(), this.supporter = const LevelInfo(), this.showCharm = true, this.showSupporter = false});
  final LevelInfo charm;
  final LevelInfo supporter;
  final bool showCharm;
  final bool showSupporter;

  factory MyLevels.fromJson(Map<String, dynamic> j) => MyLevels(
        charm: LevelInfo.fromJson(j['charm']),
        supporter: LevelInfo.fromJson(j['supporter']),
        showCharm: jsonBool(j['show_charm']),
        showSupporter: jsonBool(j['show_supporter']),
      );
}

class BoardEntry {
  const BoardEntry({required this.rank, required this.ref, required this.name, required this.level, required this.title, required this.gifts, required this.me});
  final int rank;
  final String ref;
  final String name;
  final int level;
  final String title;
  final int gifts;
  final bool me;

  factory BoardEntry.fromJson(Map<String, dynamic> j) {
    final lv = j['level'] is Map ? Map<String, dynamic>.from(j['level'] as Map) : <String, dynamic>{};
    return BoardEntry(
      rank: jsonInt(j['rank']),
      ref: jsonString(j['ref']),
      name: jsonString(j['name']),
      level: jsonInt(lv['level']),
      title: jsonString(lv['title']),
      gifts: jsonInt(j['gifts']),
      me: jsonBool(j['me']),
    );
  }
}

class Board {
  const Board({this.entries = const [], this.myRank = 0});
  final List<BoardEntry> entries;
  final int myRank;
}

class LevelsRepository {
  LevelsRepository(this._api);
  final ApiClient _api;

  Future<MyLevels> mine() async => MyLevels.fromJson(await _api.getMap('/levels/me'));

  Future<void> setVisibility({required bool charm, required bool supporter}) =>
      _api.put('/levels/me/visibility', body: {'show_charm': charm, 'show_supporter': supporter});

  /// [kind] is hosts or supporters; [period] is week, month or all.
  Future<Board> board(String kind, String period) async {
    final m = await _api.getMap('/levels/board/$kind', query: {'period': period});
    return Board(entries: listOf(m['entries'], BoardEntry.fromJson), myRank: jsonInt(m['my_rank']));
  }
}

final levelsRepositoryProvider = Provider<LevelsRepository>((ref) => LevelsRepository(ref.watch(apiClientProvider)));

final myLevelsProvider = FutureProvider.autoDispose<MyLevels>((ref) => ref.watch(levelsRepositoryProvider).mine());

final boardProvider = FutureProvider.autoDispose.family<Board, (String, String)>(
    (ref, key) => ref.watch(levelsRepositoryProvider).board(key.$1, key.$2));
