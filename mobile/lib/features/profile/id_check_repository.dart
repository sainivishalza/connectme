import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models/json.dart';
import '../../core/network/api_client.dart';
import '../../core/providers.dart';

/// Where the member stands in the human-reviewed ID check.
class IdCheckStatus {
  IdCheckStatus({required this.state, this.code = '', this.expiresAt, this.rejectReason = ''});

  /// none | awaiting | pending | verified | rejected
  final String state;
  final String code;
  final DateTime? expiresAt;
  final String rejectReason;

  factory IdCheckStatus.fromJson(Map<String, dynamic> j) => IdCheckStatus(
        state: jsonString(j['state']),
        code: jsonString(j['code']),
        expiresAt: DateTime.tryParse(jsonString(j['expires_at']))?.toLocal(),
        rejectReason: jsonString(j['reject_reason']),
      );

  /// A sentence for a rejection reason from the reviewer.
  String get rejectionText => switch (rejectReason) {
        'code_missing' => 'The code on the paper was missing or did not match.',
        'face_mismatch' => 'The face in the selfie did not match the ID.',
        'unreadable' => 'A photo was too dark or blurry to read.',
        'underage' => 'You must be 18 or over to use ConnectMe.',
        'bad_document' => 'The document was not a valid ID.',
        _ => 'The check could not be approved.',
      };
}

class IdCheckRepository {
  IdCheckRepository(this._api);
  final ApiClient _api;

  Future<IdCheckStatus> status() async => IdCheckStatus.fromJson(await _api.getMap('/idcheck/status'));

  Future<IdCheckStatus> start() async => IdCheckStatus.fromJson(await _api.postMap('/idcheck/start'));

  Future<void> submit({required Uint8List idPhoto, required Uint8List selfie}) => _api.postForm(
        '/idcheck/submit',
        FormData.fromMap({
          'id_photo': MultipartFile.fromBytes(idPhoto, filename: 'id.jpg'),
          'selfie': MultipartFile.fromBytes(selfie, filename: 'selfie.jpg'),
        }),
      );
}

final idCheckRepositoryProvider = Provider<IdCheckRepository>((ref) => IdCheckRepository(ref.watch(apiClientProvider)));

final idCheckStatusProvider = FutureProvider.autoDispose<IdCheckStatus>((ref) => ref.watch(idCheckRepositoryProvider).status());

/// Chooses a photo; the app uses the camera, tests use a fake.
typedef PhotoPicker = Future<Uint8List?> Function({required bool selfie});
