import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models/json.dart';
import '../../core/network/api_client.dart';
import '../../core/providers.dart';

class Partner {
  Partner({required this.id, required this.kind, required this.name, this.description = '', this.address = '', this.distance = ''});
  final String id;
  final String kind; // hotel | restaurant | activity
  final String name;
  final String description;
  final String address;
  final String distance;

  factory Partner.fromJson(Map<String, dynamic> j) => Partner(
        id: jsonString(j['id']),
        kind: jsonString(j['kind']),
        name: jsonString(j['name']),
        description: jsonString(j['description']),
        address: jsonString(j['address']),
        distance: jsonString(j['distance']),
      );
}

class PartnerEvent {
  PartnerEvent({
    required this.id,
    required this.partner,
    required this.kind,
    required this.title,
    required this.startsAt,
    this.description = '',
    this.capacity,
    this.going = 0,
    this.priceCoins = 0,
    this.guestsOnly = false,
    this.joined = false,
    this.distance = '',
  });
  final String id;
  final String partner;
  final String kind; // mixer | concert | hangout
  final String title;
  final String description;
  final DateTime startsAt;
  final int? capacity;
  final int going;
  final int priceCoins;
  final bool guestsOnly;
  final bool joined;
  final String distance;

  bool get full => capacity != null && going >= capacity! && !joined;

  factory PartnerEvent.fromJson(Map<String, dynamic> j) => PartnerEvent(
        id: jsonString(j['id']),
        partner: jsonString(j['partner']),
        kind: jsonString(j['kind']),
        title: jsonString(j['title']),
        description: jsonString(j['description']),
        startsAt: DateTime.tryParse(jsonString(j['starts_at']))?.toLocal() ?? DateTime.now(),
        capacity: j['capacity'] == null ? null : jsonInt(j['capacity']),
        going: jsonInt(j['going']),
        priceCoins: jsonInt(j['price_coins']),
        guestsOnly: jsonBool(j['guests_only']),
        joined: jsonBool(j['joined']),
        distance: jsonString(j['distance']),
      );
}

class Booking {
  Booking({
    required this.id,
    required this.partner,
    required this.partySize,
    required this.requestedFor,
    required this.status,
    this.note = '',
    this.amountCents,
    this.paymentStatus = 'unpaid',
  });
  final String id;
  final String partner;
  final int partySize;
  final DateTime requestedFor;
  final String status; // requested | confirmed | declined | cancelled | completed
  final String note;
  final int? amountCents; // agreed price, set when the venue confirms
  final String paymentStatus; // unpaid | paid | refunded

  bool get cancellable => status == 'requested' || status == 'confirmed';
  bool get payable => status == 'confirmed' && paymentStatus == 'unpaid' && (amountCents ?? 0) >= 50;
  String get price => amountCents == null ? '' : '\$${(amountCents! / 100).toStringAsFixed(2)}';

  factory Booking.fromJson(Map<String, dynamic> j) => Booking(
        id: jsonString(j['id']),
        partner: jsonString(j['partner']),
        partySize: jsonInt(j['party_size']),
        requestedFor: DateTime.tryParse(jsonString(j['requested_for']))?.toLocal() ?? DateTime.now(),
        status: jsonString(j['status']),
        note: jsonString(j['note']),
        amountCents: j['amount_cents'] == null ? null : jsonInt(j['amount_cents']),
        paymentStatus: jsonString(j['payment_status']).isEmpty ? 'unpaid' : jsonString(j['payment_status']),
      );
}

class PartnersRepository {
  PartnersRepository(this._api);
  final ApiClient _api;

  Future<List<Partner>> nearby() async => listOf((await _api.getMap('/partners/nearby'))['partners'], Partner.fromJson);
  Future<List<PartnerEvent>> events() async => listOf((await _api.getMap('/partners/events'))['events'], PartnerEvent.fromJson);
  Future<List<PartnerEvent>> myEvents() async => listOf((await _api.getMap('/partners/events/mine'))['events'], PartnerEvent.fromJson);

  Future<PartnerEvent> join(String id) async => PartnerEvent.fromJson(await _api.postMap('/partners/events/$id/rsvp'));
  Future<void> leave(String id) => _api.delete('/partners/events/$id/rsvp');

  Future<Booking> book(String partnerId, {required int partySize, required DateTime at, String note = ''}) async => Booking.fromJson(
        await _api.postMap('/partners/$partnerId/bookings', body: {
          'party_size': partySize,
          'requested_for': at.toUtc().toIso8601String(),
          'note': note,
        }),
      );

  Future<List<Booking>> bookings() async => listOf((await _api.getMap('/partners/bookings/mine'))['bookings'], Booking.fromJson);
  Future<void> cancelBooking(String id) => _api.delete('/partners/bookings/$id');

  /// The hosted Checkout URL for a confirmed booking (the price is the one the venue set).
  Future<String> payBooking(String id) async =>
      (await _api.postMap('/payments/bookings/$id/checkout'))['checkout_url'] as String;
}

final partnersRepositoryProvider = Provider<PartnersRepository>((ref) => PartnersRepository(ref.watch(apiClientProvider)));

final partnerEventsProvider = FutureProvider.autoDispose<List<PartnerEvent>>((ref) => ref.watch(partnersRepositoryProvider).events());
final partnerPlacesProvider = FutureProvider.autoDispose<List<Partner>>((ref) => ref.watch(partnersRepositoryProvider).nearby());
final myBookingsProvider = FutureProvider.autoDispose<List<Booking>>((ref) => ref.watch(partnersRepositoryProvider).bookings());
