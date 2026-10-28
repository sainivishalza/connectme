import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/models/json.dart' show jsonStrings;
import '../../core/network/api_client.dart';
import '../../core/providers.dart';

class GroupsRepository {
  GroupsRepository(this._api);
  final ApiClient _api;

  Future<List<Group>> mine() async => listOf((await _api.getMap('/groups'))['groups'], Group.fromJson);

  Future<Group> get(String id) async => Group.fromJson(await _api.getMap('/groups/$id'));

  Future<Group> create({
    required String name,
    required String type,
    required String activity,
    String activityLabel = '',
    int? targetSize,
    String description = '',
  }) async =>
      Group.fromJson(await _api.postMap('/groups', body: {
        'name': name,
        'type': type,
        'activity': activity,
        if (activity == 'custom') 'activity_label': activityLabel,
        if (type == 'group' && targetSize != null) 'target_size': targetSize,
        if (description.isNotEmpty) 'description': description,
      }));

  Future<void> invite(String groupId, String ref) => _api.post('/groups/$groupId/invite', body: {'ref': ref});

  Future<List<GroupInvite>> invites() async => listOf((await _api.getMap('/groups/invites'))['invites'], GroupInvite.fromJson);

  Future<void> respond(String inviteId, {required bool accept}) =>
      _api.post('/groups/invites/$inviteId/${accept ? 'accept' : 'decline'}');

  Future<void> leave(String groupId) => _api.delete('/groups/$groupId/members/me');

  Future<List<Group>> browse(String forGroup, {String? activity, int radiusKm = 25}) async => listOf(
        (await _api.getMap('/groups/browse', query: {
          'for_group': forGroup,
          'activity': ?activity,
          'radius_km': radiusKm,
        }))['groups'],
        Group.fromJson,
      );

  /// Consents to match; returns the group-match id when both sides agreed.
  Future<String?> match(String groupId, String targetId) async {
    final m = await _api.postMap('/groups/$groupId/match', body: {'target_group_id': targetId});
    return m['matched'] == true ? m['group_match_id'] as String? : null;
  }

  Future<List<GroupMatch>> matches() async => listOf((await _api.getMap('/groups/matches'))['matches'], GroupMatch.fromJson);

  Future<List<DateView>> dates(String groupMatchId) async =>
      listOf((await _api.getMap('/groups/matches/$groupMatchId/dates'))['dates'], DateView.fromJson);

  Future<void> schedule(String groupId,
          {required String groupMatchId, required String venue, String address = '', required DateTime startsAt}) =>
      _api.post('/groups/$groupId/schedule', body: {
        'group_match_id': groupMatchId,
        'venue': venue,
        'address': address,
        'starts_at': startsAt.toUtc().toIso8601String(),
      });

  Future<Logistics> logistics(String dateId) async => Logistics.fromJson(await _api.getMap('/groups/dates/$dateId/logistics'));
  Future<Logistics> payLogistics(String dateId) async => Logistics.fromJson(await _api.postMap('/groups/dates/$dateId/logistics'));

  Future<void> confirmDate(String dateId) => _api.post('/groups/dates/$dateId/confirm');
  Future<void> cancelDate(String dateId) => _api.delete('/groups/dates/$dateId');

  /// Reply ideas for a group chat. [lines] are only what the member chose to
  /// share: who ("me" or "other") and the text, nothing else.
  Future<List<String>> replyIdeas(String groupMatchId, List<({bool mine, String text})> lines) async {
    final m = await _api.postMap('/groups/matches/$groupMatchId/reply-ideas', body: {
      'lines': [for (final l in lines) {'who': l.mine ? 'me' : 'other', 'text': l.text}],
    });
    return jsonStrings(m['replies']);
  }

  Future<Suggestions> suggestions(String groupId) async =>
      Suggestions.fromJson(await _api.postMap('/groups/$groupId/venue'));

  /// Ideas for a merged chat (the chat id is a group-match id).
  Future<Suggestions> suggestionsForMatch(String groupMatchId) async {
    final m = (await matches()).where((x) => x.id == groupMatchId).firstOrNull;
    if (m == null) throw ApiException(404, 'Group chat not found');
    return suggestions(m.myGroupId);
  }
}

final groupsRepositoryProvider = Provider<GroupsRepository>((ref) => GroupsRepository(ref.watch(apiClientProvider)));

final myGroupsProvider = FutureProvider.autoDispose<List<Group>>((ref) => ref.watch(groupsRepositoryProvider).mine());
final groupInvitesProvider =
    FutureProvider.autoDispose<List<GroupInvite>>((ref) => ref.watch(groupsRepositoryProvider).invites());
final groupProvider =
    FutureProvider.autoDispose.family<Group, String>((ref, id) => ref.watch(groupsRepositoryProvider).get(id));
final groupMatchesProvider =
    FutureProvider.autoDispose<List<GroupMatch>>((ref) => ref.watch(groupsRepositoryProvider).matches());
final groupDatesProvider = FutureProvider.autoDispose.family<List<DateView>, String>(
    (ref, gm) => ref.watch(groupsRepositoryProvider).dates(gm));
