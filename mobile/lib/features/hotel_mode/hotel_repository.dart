import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/network/api_client.dart';
import '../../core/providers.dart';

class HotelRepository {
  HotelRepository(this._api);
  final ApiClient _api;

  /// Venues whose geofence contains the last location shared with the server.
  Future<List<Hotel>> detect() async => listOf((await _api.getMap('/geo/detect'))['hotels'], Hotel.fromJson);

  /// The active stay, or null when not checked in.
  Future<CheckIn?> current() async {
    try {
      return CheckIn.fromJson(await _api.getMap('/geo/checkin'));
    } on ApiException catch (e) {
      if (e.status == 403 || e.status == 404) return null;
      rethrow;
    }
  }

  Future<CheckIn> checkIn(String hotelId) async =>
      CheckIn.fromJson(await _api.postMap('/geo/checkin', body: {'hotel_id': hotelId}));

  Future<void> checkOut() => _api.post('/geo/checkout');

  Future<List<PublicProfile>> feed(String hotelId, {String mood = ''}) async => listOf(
        (await _api.getMap('/geo/hotels/$hotelId/users', query: {if (mood.isNotEmpty) 'mood': mood}))['users'],
        PublicProfile.fromJson,
      );

  /// Sets (or with '' clears) my mood for this stay.
  Future<void> setMood(String mood) => _api.put('/geo/checkin/mood', body: {'mood': mood});

  Future<List<BoardPost>> board(String hotelId) async =>
      listOf((await _api.getMap('/geo/hotels/$hotelId/board'))['posts'], BoardPost.fromJson);

  Future<void> post(String hotelId, String message) =>
      _api.post('/geo/hotels/$hotelId/board', body: {'message': message});

  Future<void> sendDrink(String hotelId, String toRef) =>
      _api.post('/geo/hotels/$hotelId/drink', body: {'to_ref': toRef});

  Future<List<String>> drinksFrom(String hotelId) async => [
        for (final d in ((await _api.getMap('/geo/hotels/$hotelId/drinks'))['drinks'] as List? ?? []).whereType<Map>())
          d['from_ref'] as String? ?? '',
      ];
}

final hotelRepositoryProvider = Provider<HotelRepository>((ref) => HotelRepository(ref.watch(apiClientProvider)));

final currentStayProvider = FutureProvider.autoDispose<CheckIn?>((ref) => ref.watch(hotelRepositoryProvider).current());
final detectedHotelsProvider =
    FutureProvider.autoDispose<List<Hotel>>((ref) => ref.watch(hotelRepositoryProvider).detect());
/// Which guest mood the hotel feed is filtered to ('' = everyone).
class HotelMoodFilter extends Notifier<String> {
  @override
  String build() => '';
  void set(String mood) => state = mood;
}

final hotelMoodFilterProvider = NotifierProvider<HotelMoodFilter, String>(HotelMoodFilter.new);

final hotelFeedProvider = FutureProvider.autoDispose.family<List<PublicProfile>, String>(
    (ref, id) => ref.watch(hotelRepositoryProvider).feed(id, mood: ref.watch(hotelMoodFilterProvider)));
final hotelBoardProvider =
    FutureProvider.autoDispose.family<List<BoardPost>, String>((ref, id) => ref.watch(hotelRepositoryProvider).board(id));
final hotelDrinksProvider =
    FutureProvider.autoDispose.family<List<String>, String>((ref, id) => ref.watch(hotelRepositoryProvider).drinksFrom(id));
