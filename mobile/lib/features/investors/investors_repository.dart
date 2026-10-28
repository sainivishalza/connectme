import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models/json.dart';
import '../../core/network/api_client.dart';
import '../../core/providers.dart';

/// One line of the public ownership board: a name and a percentage, nothing else.
class Holder {
  const Holder({required this.rank, required this.name, required this.percent, this.founder = false});
  final int rank;
  final String name;
  final String percent;
  final bool founder;

  factory Holder.fromJson(Map<String, dynamic> j) =>
      Holder(rank: jsonInt(j['rank']), name: jsonString(j['name']), percent: jsonString(j['percent']), founder: jsonBool(j['founder']));
}

class OwnershipBoard {
  const OwnershipBoard({
    this.holders = const [],
    this.poolPercent = '49',
    this.paymentNote = '',
    this.model = 'prorata',
    this.pricePerPercent = '1000',
    this.openPercent = '49',
  });
  final List<Holder> holders;
  final String poolPercent;
  final String paymentNote;

  /// "prorata": new money lowers everyone's percentage. "fixed": a set price
  /// per 1%, no dilution, and the pool sells out.
  final String model;
  final String pricePerPercent;
  final String openPercent;
  bool get fixed => model == 'fixed';
}

class MyPledge {
  const MyPledge({required this.id, required this.amount, required this.status, required this.reference});
  final String id;
  final String amount;
  final String status;
  final String reference;

  factory MyPledge.fromJson(Map<String, dynamic> j) =>
      MyPledge(id: jsonString(j['id']), amount: jsonString(j['amount_usd']), status: jsonString(j['status']), reference: jsonString(j['reference']));
}

class MyInvestments {
  const MyInvestments({this.pledges = const [], this.percent = '0', this.paymentNote = ''});
  final List<MyPledge> pledges;
  final String percent;
  final String paymentNote;
}

class InvestorsRepository {
  InvestorsRepository(this._api);
  final ApiClient _api;

  Future<OwnershipBoard> board() async {
    final m = await _api.getMap('/investors/public');
    return OwnershipBoard(
      holders: listOf(m['holders'], Holder.fromJson),
      poolPercent: jsonString(m['pool_percent']).isEmpty ? '49' : jsonString(m['pool_percent']),
      paymentNote: jsonString(m['payment_note']),
      model: jsonString(m['model']).isEmpty ? 'prorata' : jsonString(m['model']),
      pricePerPercent: jsonString(m['price_per_percent']).isEmpty ? '1000' : jsonString(m['price_per_percent']),
      openPercent: jsonString(m['open_percent']).isEmpty ? '49' : jsonString(m['open_percent']),
    );
  }

  Future<MyInvestments> mine() async {
    final m = await _api.getMap('/investors/mine');
    return MyInvestments(
      pledges: listOf(m['pledges'], MyPledge.fromJson),
      percent: jsonString(m['percent']).isEmpty ? '0' : jsonString(m['percent']),
      paymentNote: jsonString(m['payment_note']),
    );
  }

  Future<MyPledge> pledge(String amountUsd, {required bool showName}) async => MyPledge.fromJson(
        await _api.postMap('/investors/pledges', body: {'amount_usd': amountUsd, 'show_name': showName, 'accept_terms': true}),
      );

  Future<void> cancel(String id) => _api.delete('/investors/pledges/$id');
}

final investorsRepositoryProvider = Provider<InvestorsRepository>((ref) => InvestorsRepository(ref.watch(apiClientProvider)));

final ownershipBoardProvider = FutureProvider.autoDispose<OwnershipBoard>((ref) => ref.watch(investorsRepositoryProvider).board());

final myInvestmentsProvider = FutureProvider.autoDispose<MyInvestments>((ref) => ref.watch(investorsRepositoryProvider).mine());
