import 'json.dart';

class Hotel {
  Hotel({required this.id, required this.name});
  final String id;
  final String name;
  factory Hotel.fromJson(Map<String, dynamic> j) => Hotel(id: jsonString(j['id']), name: jsonString(j['name']));
}

class CheckIn {
  CheckIn({required this.hotel, required this.checkedInAt, required this.dataDeletedAt, this.mood = ''});
  final Hotel hotel;
  final String mood; // business | company | guide | ''
  final DateTime checkedInAt;
  final DateTime dataDeletedAt;

  factory CheckIn.fromJson(Map<String, dynamic> j) => CheckIn(
        hotel: Hotel.fromJson(Map<String, dynamic>.from(j['hotel'] as Map? ?? {})),
        checkedInAt: DateTime.tryParse(jsonString(j['checked_in_at']))?.toLocal() ?? DateTime.now(),
        dataDeletedAt: DateTime.tryParse(jsonString(j['data_deleted_at']))?.toLocal() ?? DateTime.now(),
        mood: jsonString(j['mood']),
      );
}

class BoardPost {
  BoardPost({required this.id, required this.authorRef, required this.message, required this.createdAt});
  final String id;
  final String authorRef;
  final String message;
  final DateTime createdAt;

  factory BoardPost.fromJson(Map<String, dynamic> j) => BoardPost(
        id: jsonString(j['id']),
        authorRef: jsonString(j['author_ref']),
        message: jsonString(j['message']),
        createdAt: DateTime.tryParse(jsonString(j['created_at']))?.toLocal() ?? DateTime.now(),
      );
}

class Perks {
  Perks({
    this.unlimitedLikes = false,
    this.superLikesPerDay = 0,
    this.seeWhoLiked = false,
    this.invisibility = false,
    this.vipBadge = false,
  });
  final bool unlimitedLikes;
  final int superLikesPerDay;
  final bool seeWhoLiked;
  final bool invisibility;
  final bool vipBadge;

  factory Perks.fromJson(Map<String, dynamic> j) => Perks(
        unlimitedLikes: jsonBool(j['unlimited_likes']),
        superLikesPerDay: jsonInt(j['super_likes_per_day']),
        seeWhoLiked: jsonBool(j['see_who_liked_you']),
        invisibility: jsonBool(j['invisibility']),
        vipBadge: jsonBool(j['vip_badge']),
      );
}

class CoinPack {
  CoinPack({required this.id, required this.coins, required this.priceCents});
  final String id;
  final int coins;
  final int priceCents;
  String get price => '\$${(priceCents / 100).toStringAsFixed(2)}';

  factory CoinPack.fromJson(Map<String, dynamic> j) =>
      CoinPack(id: jsonString(j['id']), coins: jsonInt(j['coins']), priceCents: jsonInt(j['price_cents']));
}

class Account {
  Account({
    required this.coins,
    required this.tier,
    required this.perks,
    required this.packs,
    this.superLikesLeft = 0,
    this.freeBoostReady = false,
    this.likesLeftToday,
    this.cancelsAtEnd = false,
    this.endsAt,
  });
  final int coins;
  final String tier;
  final Perks perks;
  final List<CoinPack> packs;
  final int superLikesLeft;
  final bool freeBoostReady;
  final int? likesLeftToday;
  final bool cancelsAtEnd;
  final DateTime? endsAt;

  factory Account.fromJson(Map<String, dynamic> j) => Account(
        coins: jsonInt(j['coins']),
        tier: jsonString(j['tier']),
        perks: Perks.fromJson(Map<String, dynamic>.from(j['perks'] as Map? ?? {})),
        packs: jsonMaps(j['coin_packs']).map(CoinPack.fromJson).toList(),
        superLikesLeft: jsonInt(j['super_likes_left_today']),
        freeBoostReady: jsonBool(j['free_boost_available']),
        likesLeftToday: j['likes_left_today'] is num ? (j['likes_left_today'] as num).toInt() : null,
        cancelsAtEnd: jsonBool(j['cancels_at_period_end']),
        endsAt: DateTime.tryParse(jsonString(j['subscription_ends_at']))?.toLocal(),
      );
}

class Gift {
  Gift({required this.id, required this.name, required this.coins});
  final String id;
  final String name;
  final int coins;
  factory Gift.fromJson(Map<String, dynamic> j) => Gift(id: jsonString(j['id']), name: jsonString(j['name']), coins: jsonInt(j['price_coins']));
}

class ReceivedGift {
  ReceivedGift({required this.giftId, required this.name, required this.fromRef, required this.createdAt});
  final String giftId;
  final String name;
  final String fromRef;
  final DateTime createdAt;

  factory ReceivedGift.fromJson(Map<String, dynamic> j) => ReceivedGift(
        giftId: jsonString(j['gift_id']),
        name: jsonString(j['name']),
        fromRef: jsonString(j['from_ref']),
        createdAt: DateTime.tryParse(jsonString(j['created_at']))?.toLocal() ?? DateTime.now(),
      );
}
