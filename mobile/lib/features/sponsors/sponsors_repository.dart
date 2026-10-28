import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models/json.dart';
import '../../core/network/api_client.dart';
import '../../core/providers.dart';

class Sponsored {
  Sponsored({required this.id, required this.advertiser, required this.headline, this.body = '', this.linkUrl = ''});
  final String id;
  final String advertiser;
  final String headline;
  final String body;
  final String linkUrl;

  factory Sponsored.fromJson(Map<String, dynamic> j) => Sponsored(
        id: jsonString(j['id']),
        advertiser: jsonString(j['advertiser']),
        headline: jsonString(j['headline']),
        body: jsonString(j['body']),
        linkUrl: jsonString(j['link_url']),
      );
}

class SponsorsRepository {
  SponsorsRepository(this._api);
  final ApiClient _api;

  /// At most one placement, or null (Silver and above, or nothing eligible).
  Future<Sponsored?> feed() async {
    final m = await _api.getMap('/sponsors/feed');
    final p = m['placement'];
    return p is Map ? Sponsored.fromJson(Map<String, dynamic>.from(p)) : null;
  }

  /// Counts the click and returns the link to open.
  Future<String> click(String id) async => jsonString((await _api.postMap('/sponsors/$id/click'))['link_url']);
}

final sponsorsRepositoryProvider = Provider<SponsorsRepository>((ref) => SponsorsRepository(ref.watch(apiClientProvider)));
