import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../core/models.dart';
import '../../core/network/api_client.dart';
import '../../core/providers.dart';

class PaymentsRepository {
  PaymentsRepository(this._api);
  final ApiClient _api;

  Future<Account> account() async => Account.fromJson(await _api.getMap('/payments/balance'));

  /// Returns the hosted Checkout URL for a tier (price is chosen server-side).
  Future<String> subscribe(String tier) async =>
      (await _api.postMap('/payments/subscribe', body: {'tier': tier}))['checkout_url'] as String;

  Future<String> buyCoins(String packId) async =>
      (await _api.postMap('/payments/coins', body: {'pack_id': packId}))['checkout_url'] as String;

  Future<void> cancel() => _api.post('/payments/cancel');

  Future<List<Gift>> catalog() async => listOf((await _api.getMap('/payments/gifts/catalog'))['gifts'], Gift.fromJson);

  Future<List<ReceivedGift>> received() async =>
      listOf((await _api.getMap('/payments/gifts'))['gifts'], ReceivedGift.fromJson);

  Future<void> sendGift({required String toRef, required String giftId, required String requestId}) =>
      _api.post('/payments/gift', body: {'to_ref': toRef, 'gift_id': giftId, 'request_id': requestId});
}

final paymentsRepositoryProvider = Provider<PaymentsRepository>((ref) => PaymentsRepository(ref.watch(apiClientProvider)));

final accountProvider = FutureProvider.autoDispose<Account>((ref) => ref.watch(paymentsRepositoryProvider).account());

final giftCatalogProvider =
    FutureProvider.autoDispose<List<Gift>>((ref) => ref.watch(paymentsRepositoryProvider).catalog());

final receivedGiftsProvider =
    FutureProvider.autoDispose<List<ReceivedGift>>((ref) => ref.watch(paymentsRepositoryProvider).received());

/// Opens a Checkout URL outside the app. Abstracted so tests need no platform.
abstract class UrlOpener {
  Future<bool> open(String url);
}

class SystemUrlOpener implements UrlOpener {
  @override
  Future<bool> open(String url) => launchUrl(Uri.parse(url), mode: LaunchMode.externalApplication);
}

final urlOpenerProvider = Provider<UrlOpener>((ref) => SystemUrlOpener());
