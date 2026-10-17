import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:geolocator/geolocator.dart';

import '../../core/models.dart';
import '../../core/network/api_client.dart';
import '../../core/providers.dart';

class ProfileRepository {
  ProfileRepository(this._api);
  final ApiClient _api;

  Future<OwnProfile> me() async => OwnProfile.fromJson(await _api.getMap('/users/me'));

  Future<OwnProfile> update(Map<String, dynamic> changes) async =>
      OwnProfile.fromJson(await _api.patchMap('/users/me', body: changes));

  Future<OwnProfile> setMode(String mode) async =>
      OwnProfile.fromJson(await _api.postMap('/users/me/mode', body: {'mode': mode}));

  Future<OwnProfile> setInvisible(bool enabled) async =>
      OwnProfile.fromJson(await _api.postMap('/users/me/invisible', body: {'enabled': enabled}));

  Future<void> setVoiceIntro(Uint8List bytes, int seconds) =>
      _api.putBytes('/users/me/voice-intro', bytes, query: {'seconds': seconds});

  Future<void> deleteVoiceIntro() => _api.delete('/users/me/voice-intro');

  Future<void> setLocation(double lat, double lng) => _api.put('/users/me/location', body: {'lat': lat, 'lng': lng});

  Future<void> uploadPhoto(Uint8List bytes, String filename) => _api.postForm(
        '/users/me/photos',
        FormData.fromMap({'photo': MultipartFile.fromBytes(bytes, filename: filename)}),
      );

  Future<void> deletePhoto(String id) => _api.delete('/users/me/photos/$id');

  /// The user's data export as pretty JSON text.
  Future<String> exportData() async =>
      const JsonEncoder.withIndent('  ').convert(await _api.getMap('/privacy/export'));
}

final profileRepositoryProvider = Provider<ProfileRepository>((ref) => ProfileRepository(ref.watch(apiClientProvider)));

final meProvider = FutureProvider.autoDispose<OwnProfile>((ref) => ref.watch(profileRepositoryProvider).me());

/// Device location access, behind an interface so screens can be tested.
abstract class LocationService {
  /// Returns the current position or throws [LocationException] with a user-facing reason.
  Future<({double lat, double lng})> current();
}

class LocationException implements Exception {
  LocationException(this.message);
  final String message;
  @override
  String toString() => message;
}

class GeolocatorService implements LocationService {
  @override
  Future<({double lat, double lng})> current() async {
    if (!await Geolocator.isLocationServiceEnabled()) {
      throw LocationException('Turn on location services to use this feature.');
    }
    var perm = await Geolocator.checkPermission();
    if (perm == LocationPermission.denied) perm = await Geolocator.requestPermission();
    if (perm == LocationPermission.denied || perm == LocationPermission.deniedForever) {
      throw LocationException('Location permission is needed. You can enable it in Settings.');
    }
    final p = await Geolocator.getCurrentPosition(
      locationSettings: const LocationSettings(accuracy: LocationAccuracy.high, timeLimit: Duration(seconds: 15)),
    );
    return (lat: p.latitude, lng: p.longitude);
  }
}

final locationServiceProvider = Provider<LocationService>((ref) => GeolocatorService());

/// Reads the device position and stores it on the server (which keeps the
/// exact point private and only ever exposes coarse distance).
Future<void> shareLocation(WidgetRef ref) async {
  final pos = await ref.read(locationServiceProvider).current();
  await ref.read(profileRepositoryProvider).setLocation(pos.lat, pos.lng);
}
