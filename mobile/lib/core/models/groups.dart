import 'json.dart';
import 'profile.dart';

class Group {
  Group({
    required this.id,
    required this.name,
    required this.type,
    required this.activity,
    required this.targetSize,
    required this.memberCount,
    required this.ready,
    required this.locked,
    this.activityLabel = '',
    this.description = '',
    this.distance = '',
    this.members = const [],
  });

  final String id;
  final String name;
  final String type;
  final String activity;
  final String activityLabel;
  final String description;
  final int targetSize;
  final int memberCount;
  final bool ready;
  final bool locked;
  final String distance;
  final List<PublicProfile> members;

  String get activityName => activity == 'custom' && activityLabel.isNotEmpty ? activityLabel : activity.replaceAll('_', ' ');

  factory Group.fromJson(Map<String, dynamic> j) => Group(
        id: jsonString(j['id']),
        name: jsonString(j['name']),
        type: jsonString(j['type']),
        activity: jsonString(j['activity']),
        activityLabel: jsonString(j['activity_label']),
        description: jsonString(j['description']),
        targetSize: jsonInt(j['target_size']),
        memberCount: jsonInt(j['member_count']),
        ready: jsonBool(j['ready']),
        locked: jsonBool(j['locked']),
        distance: jsonString(j['distance']),
        members: jsonMaps(j['members']).map(PublicProfile.fromJson).toList(),
      );
}

class GroupInvite {
  GroupInvite({required this.id, required this.groupId, required this.groupName, required this.activity, required this.type});
  final String id;
  final String groupId;
  final String groupName;
  final String activity;
  final String type;

  factory GroupInvite.fromJson(Map<String, dynamic> j) => GroupInvite(
        id: jsonString(j['id']),
        groupId: jsonString(j['group_id']),
        groupName: jsonString(j['group_name']),
        activity: jsonString(j['activity']),
        type: jsonString(j['type']),
      );
}

class GroupMatch {
  GroupMatch({required this.id, required this.myGroupId, required this.other});
  final String id;
  final String myGroupId;
  final Group other;

  factory GroupMatch.fromJson(Map<String, dynamic> j) => GroupMatch(
        id: jsonString(j['id']),
        myGroupId: jsonString(j['my_group_id']),
        other: Group.fromJson(Map<String, dynamic>.from(j['other_group'] as Map? ?? {})),
      );
}

class DateView {
  DateView({required this.id, required this.venue, required this.startsAt, required this.status, required this.iConfirmed, this.address = ''});
  final String id;
  final String venue;
  final String address;
  final DateTime startsAt;
  final String status;
  final bool iConfirmed;

  factory DateView.fromJson(Map<String, dynamic> j) => DateView(
        id: jsonString(j['id']),
        venue: jsonString(j['venue']),
        address: jsonString(j['address']),
        startsAt: DateTime.tryParse(jsonString(j['starts_at']))?.toLocal() ?? DateTime.now(),
        status: jsonString(j['status']),
        iConfirmed: jsonBool(j['i_confirmed']),
      );
}

class Suggestions {
  Suggestions({required this.venues, required this.icebreakers, required this.source, this.plan = const []});
  final List<String> venues;
  final List<String> icebreakers;
  final List<String> plan; // a short outing plan; only the AI wingman fills it
  final String source;

  factory Suggestions.fromJson(Map<String, dynamic> j) =>
      Suggestions(venues: jsonStrings(j['venues']), icebreakers: jsonStrings(j['icebreakers']), plan: jsonStrings(j['plan']), source: jsonString(j['source']));
}

class Logistics {
  Logistics({required this.priceCoins, required this.paid, this.plan});
  final int priceCoins;
  final bool paid;
  final Suggestions? plan;

  factory Logistics.fromJson(Map<String, dynamic> j) => Logistics(
        priceCoins: jsonInt(j['price_coins']),
        paid: jsonBool(j['paid']),
        plan: j['plan'] is Map ? Suggestions.fromJson(Map<String, dynamic>.from(j['plan'] as Map)) : null,
      );
}
