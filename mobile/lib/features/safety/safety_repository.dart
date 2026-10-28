import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models/json.dart';
import '../../core/network/api_client.dart';
import '../../core/providers.dart';

class TrustedContact {
  const TrustedContact({required this.id, required this.name, required this.email, required this.confirmed, this.phone = '', this.phoneConfirmed = false});
  final String id;
  final String name;
  final String email;
  final bool confirmed;

  /// The number as the server shows it (last two digits only), and whether the
  /// contact read the texted code back, which is what turns text alerts on.
  final String phone;
  final bool phoneConfirmed;

  factory TrustedContact.fromJson(Map<String, dynamic> j) => TrustedContact(
        id: jsonString(j['id']),
        name: jsonString(j['name']),
        email: jsonString(j['email']),
        confirmed: jsonBool(j['confirmed']),
        phone: jsonString(j['phone']),
        phoneConfirmed: jsonBool(j['phone_confirmed']),
      );
}

class DatePlan {
  const DatePlan({required this.id, required this.contact, required this.place, required this.meetAt, required this.checkinBy, required this.status, this.who = '', this.canExtend = false});
  final String id;
  final String contact;
  final String place;
  final DateTime meetAt;
  final DateTime checkinBy;
  final String status; // active | safe | alerted | cancelled
  final String who;
  final bool canExtend;

  bool get active => status == 'active';
  bool get open => status == 'active' || status == 'alerted';

  factory DatePlan.fromJson(Map<String, dynamic> j) => DatePlan(
        id: jsonString(j['id']),
        contact: jsonString(j['contact']),
        place: jsonString(j['place']),
        meetAt: DateTime.tryParse(jsonString(j['meet_at']))?.toLocal() ?? DateTime.now(),
        checkinBy: DateTime.tryParse(jsonString(j['checkin_by']))?.toLocal() ?? DateTime.now(),
        status: jsonString(j['status']),
        who: jsonString(j['who']),
        canExtend: jsonBool(j['can_extend']),
      );
}

/// A report you filed. [outcome] is pending, action_taken or no_action; the
/// server never says who was reported.
class MyReport {
  const MyReport({required this.id, required this.reason, required this.createdAt, required this.outcome});
  final String id;
  final String reason;
  final DateTime createdAt;
  final String outcome;

  factory MyReport.fromJson(Map<String, dynamic> j) => MyReport(
        id: jsonString(j['id']),
        reason: jsonString(j['reason']),
        createdAt: DateTime.tryParse(jsonString(j['created_at']))?.toLocal() ?? DateTime.now(),
        outcome: jsonString(j['outcome']),
      );
}

class SafetyRepository {
  SafetyRepository(this._api);
  final ApiClient _api;

  Future<List<TrustedContact>> contacts() async => listOf((await _api.getMap('/safety/contacts'))['contacts'], TrustedContact.fromJson);
  Future<void> addContact(String name, String email) => _api.post('/safety/contacts', body: {'name': name, 'email': email});
  Future<void> removeContact(String id) => _api.delete('/safety/contacts/$id');
  Future<void> setPhone(String id, String phone) => _api.put('/safety/contacts/$id/phone', body: {'phone': phone});
  Future<void> confirmPhone(String id, String code) => _api.post('/safety/contacts/$id/phone/confirm', body: {'code': code});
  Future<void> removePhone(String id) => _api.delete('/safety/contacts/$id/phone');
  Future<List<MyReport>> myReports() async => listOf((await _api.getMap('/moderation/reports/mine'))['reports'], MyReport.fromJson);
  Future<void> confirm(String token) => _api.post('/safety/confirm', body: {'token': token});

  Future<List<DatePlan>> plans() async => listOf((await _api.getMap('/safety/plans'))['plans'], DatePlan.fromJson);

  Future<void> createPlan({
    required String contactId,
    required String place,
    required DateTime meetAt,
    required int checkinAfterMin,
    String? matchId,
    bool shareName = false,
  }) =>
      _api.post('/safety/plans', body: {
        'contact_id': contactId,
        'place': place,
        'meet_at': meetAt.toUtc().toIso8601String(),
        'checkin_after_min': checkinAfterMin,
        'match_id': ?matchId,
        'share_name': shareName,
      });

  Future<void> safe(String id) => _api.post('/safety/plans/$id/safe');
  Future<void> cancel(String id) => _api.post('/safety/plans/$id/cancel');
  Future<void> extend(String id, int minutes) => _api.post('/safety/plans/$id/extend', body: {'minutes': minutes});
}

final safetyRepositoryProvider = Provider<SafetyRepository>((ref) => SafetyRepository(ref.watch(apiClientProvider)));

final trustedContactsProvider = FutureProvider.autoDispose<List<TrustedContact>>((ref) => ref.watch(safetyRepositoryProvider).contacts());
final datePlansProvider = FutureProvider.autoDispose<List<DatePlan>>((ref) => ref.watch(safetyRepositoryProvider).plans());
final myReportsProvider = FutureProvider.autoDispose<List<MyReport>>((ref) => ref.watch(safetyRepositoryProvider).myReports());
