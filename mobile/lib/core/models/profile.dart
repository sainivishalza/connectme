import 'json.dart';

/// Another person's profile exactly as the server chose to show it.
class PublicProfile {
  PublicProfile({
    required this.ref,
    required this.anonymous,
    required this.mode,
    this.displayName = '',
    this.bio = '',
    this.age,
    this.photos = const [],
    this.tags = const [],
    this.intro = '',
    this.mood = '',
    this.distance = '',
    this.hotelName = '',
    this.travelMood = '',
    this.vip = false,
    this.voiceIntroS = 0,
    this.reelS = 0,
    this.chapters = 0,
  });

  final String ref;
  final bool anonymous;
  final String mode;
  final String displayName;
  final String bio;
  final int? age;
  final List<String> photos;
  final List<String> tags;
  final String intro;
  final String mood;
  final String distance;
  final String hotelName;
  final String travelMood; // hotel feed only: business | company | guide
  final bool vip;

  /// Length of the person's voice intro in seconds (0 = none). Never set for anonymous profiles.
  final int voiceIntroS;
  final int reelS; // profile video length in seconds (0 = none)
  final int chapters; // narrative chapters on the profile (0 = none)

  /// Name to show: the real name only when the server revealed it.
  String get title => anonymous || displayName.isEmpty ? 'Anonymous' : displayName;

  factory PublicProfile.fromJson(Map<String, dynamic> j) => PublicProfile(
        ref: jsonString(j['ref']),
        anonymous: jsonBool(j['anonymous']),
        mode: jsonString(j['mode']),
        displayName: jsonString(j['display_name']),
        bio: jsonString(j['bio']),
        age: j['age'] is num ? (j['age'] as num).toInt() : null,
        photos: jsonStrings(j['photos']),
        tags: jsonStrings(j['tags']),
        intro: jsonString(j['intro']),
        mood: jsonString(j['mood']),
        distance: jsonString(j['distance']),
        hotelName: jsonString(j['hotel_name']),
        travelMood: jsonString(j['travel_mood']),
        vip: jsonBool(j['vip']),
        voiceIntroS: jsonInt(j['voice_intro_s']),
        reelS: jsonInt(j['reel_s']),
        chapters: jsonInt(j['chapters']),
      );
}

class OwnPhoto {
  OwnPhoto(this.id);
  final String id;
  String get path => '/media/photos/$id';
}

class OwnProfile {
  OwnProfile({
    required this.ref,
    required this.mode,
    this.displayName = '',
    this.bio = '',
    this.intro = '',
    this.age = 0,
    this.gender = '',
    this.lookingFor = '',
    this.mood = '',
    this.tags = const [],
    this.photos = const [],
    this.hasLocation = false,
    this.invisible = false,
    this.emailVerified = false,
    this.idVerified = false,
    this.voiceIntroS = 0,
  });

  final String ref;
  final String mode;
  final String displayName;
  final String bio;
  final String intro;
  final int age;
  final String gender;
  final String lookingFor;
  final String mood;
  final List<String> tags;
  final List<OwnPhoto> photos;
  final bool hasLocation;
  final bool invisible;
  final bool emailVerified;
  final bool idVerified;
  final int voiceIntroS;

  factory OwnProfile.fromJson(Map<String, dynamic> j) => OwnProfile(
        ref: jsonString(j['ref']),
        mode: jsonString(j['mode']),
        displayName: jsonString(j['display_name']),
        bio: jsonString(j['bio']),
        intro: jsonString(j['intro']),
        age: jsonInt(j['age']),
        gender: jsonString(j['gender']),
        lookingFor: jsonString(j['looking_for']),
        mood: jsonString(j['mood']),
        tags: jsonStrings(j['tags']),
        photos: jsonMaps(j['photos']).map((p) => OwnPhoto(jsonString(p['id']))).toList(),
        hasLocation: jsonBool(j['has_location']),
        invisible: jsonBool(j['invisible']),
        emailVerified: jsonBool(j['email_verified']),
        idVerified: jsonBool(j['id_verified']),
        voiceIntroS: jsonInt(j['voice_intro_s']),
      );
}

class MatchView {
  MatchView({
    required this.id,
    required this.mode,
    required this.profile,
    required this.revealedByMe,
    this.compatScore,
    this.compatReasons = const [],
    this.randomExpiresAt,
    this.randomKeptByMe = false,
  });
  final String id;
  final String mode;
  final PublicProfile profile;
  final bool revealedByMe;
  final int? compatScore;
  final List<String> compatReasons;

  /// Set while this is an anonymous random chat that will end unless both keep it.
  final DateTime? randomExpiresAt;
  final bool randomKeptByMe;

  factory MatchView.fromJson(Map<String, dynamic> j) => MatchView(
        id: jsonString(j['match_id']),
        mode: jsonString(j['mode']),
        revealedByMe: jsonBool(j['revealed_by_me']),
        compatScore: j['compat_score'] is num ? (j['compat_score'] as num).toInt() : null,
        compatReasons: jsonStrings(j['compat_reasons']),
        randomExpiresAt: j['random'] is Map ? DateTime.tryParse(jsonString((j['random'] as Map)['expires_at'])) : null,
        randomKeptByMe: j['random'] is Map && jsonBool((j['random'] as Map)['kept_by_me']),
        profile: PublicProfile.fromJson(Map<String, dynamic>.from(j['profile'] as Map? ?? {})),
      );
}
