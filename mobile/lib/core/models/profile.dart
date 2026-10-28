import 'details.dart';
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
    this.photoCaptions = const [],
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
    this.idVerified = false,
    this.details,
    this.prompts = const [],
    this.charmLevel = 0,
    this.quizMatch = 0,
    this.followers = 0,
    this.following = false,
  });

  /// Charm level (2 and up) from gifts received; 0 = not shown. Never on anonymous profiles.
  final int charmLevel;

  /// Quiz match with the viewer (percent, rounded to ten); 0 = not enough shared answers.
  final int quizMatch;

  /// How many people follow this member, and whether the viewer does.
  final int followers;
  final bool following;

  /// A moderator checked this member's ID. Shown on anonymous cards too.
  final bool idVerified;

  /// Lifestyle answers always; identifying ones only on full profiles.
  final ProfileDetails? details;

  /// Free-text prompt answers, on full profiles only.
  final List<PromptAnswer> prompts;

  final String ref;
  final bool anonymous;
  final String mode;
  final String displayName;
  final String bio;
  final int? age;
  final List<String> photos;
  /// One line per photo, "" where there is none; empty when no photo has one.
  final List<String> photoCaptions;
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
        photoCaptions: jsonStrings(j['photo_captions']),
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
        idVerified: jsonBool(j['id_verified']),
        charmLevel: jsonInt(j['charm_level']),
        quizMatch: jsonInt(j['quiz_match']),
        followers: jsonInt(j['followers']),
        following: jsonBool(j['following']),
        details: j['details'] is Map ? ProfileDetails.fromJson(Map<String, dynamic>.from(j['details'] as Map)) : null,
        prompts: listOf(j['prompts'], PromptAnswer.fromJson),
      );
}

class OwnPhoto {
  OwnPhoto(this.id, [this.caption = '']);
  final String id;
  final String caption;
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
    this.details = const ProfileDetails(),
    this.prompts = const [],
    this.completeness = const Completeness(),
    this.onboarded = true,
  });

  /// False until the welcome steps are finished or skipped.
  final bool onboarded;

  final ProfileDetails details;
  final List<PromptAnswer> prompts;
  final Completeness completeness;

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
        photos: jsonMaps(j['photos']).map((p) => OwnPhoto(jsonString(p['id']), jsonString(p['caption']))).toList(),
        hasLocation: jsonBool(j['has_location']),
        invisible: jsonBool(j['invisible']),
        emailVerified: jsonBool(j['email_verified']),
        idVerified: jsonBool(j['id_verified']),
        voiceIntroS: jsonInt(j['voice_intro_s']),
        details: j['details'] is Map ? ProfileDetails.fromJson(Map<String, dynamic>.from(j['details'] as Map)) : const ProfileDetails(),
        prompts: listOf(j['prompts'], PromptAnswer.fromJson),
        onboarded: j['onboarded'] != false,
        completeness: j['completeness'] is Map ? Completeness.fromJson(Map<String, dynamic>.from(j['completeness'] as Map)) : const Completeness(),
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
    this.voice,
    this.revealTimer,
    this.myComment = '',
    this.theirComment = '',
  });

  /// The notes sent with each side's like ('' = none, or the sender is anonymous).
  final VoiceStepInfo? voice;
  final RevealTimerInfo? revealTimer;
  final String myComment;
  final String theirComment;
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
        myComment: jsonString(j['my_comment']),
        theirComment: jsonString(j['their_comment']),
        randomExpiresAt: j['random'] is Map ? DateTime.tryParse(jsonString((j['random'] as Map)['expires_at'])) : null,
        revealTimer: j['reveal_timer'] is Map ? RevealTimerInfo.fromJson(Map<String, dynamic>.from(j['reveal_timer'] as Map)) : null,
        voice: j['voice'] is Map ? VoiceStepInfo.fromJson(Map<String, dynamic>.from(j['voice'] as Map)) : null,
        randomKeptByMe: j['random'] is Map && jsonBool((j['random'] as Map)['kept_by_me']),
        profile: PublicProfile.fromJson(Map<String, dynamic>.from(j['profile'] as Map? ?? {})),
      );
}

/// Where an anonymous match stands on swapping voice intros before photos.
class VoiceStepInfo {
  const VoiceStepInfo({this.byMe = false, this.unlocked = false, this.seconds = 0});
  final bool byMe;
  final bool unlocked;
  final int seconds;

  factory VoiceStepInfo.fromJson(Map<String, dynamic> j) =>
      VoiceStepInfo(byMe: jsonBool(j['by_me']), unlocked: jsonBool(j['unlocked']), seconds: jsonInt(j['seconds']));
}

/// A timed reveal agreed or proposed in an anonymous match.
class RevealTimerInfo {
  const RevealTimerInfo({required this.at, this.byMe = false, this.accepted = false, this.needsReply = false});
  final DateTime at;
  final bool byMe;
  final bool accepted;
  final bool needsReply;

  factory RevealTimerInfo.fromJson(Map<String, dynamic> j) => RevealTimerInfo(
        at: DateTime.tryParse(jsonString(j['at']))?.toLocal() ?? DateTime.now(),
        byMe: jsonBool(j['by_me']),
        accepted: jsonBool(j['accepted']),
        needsReply: jsonBool(j['needs_reply']),
      );
}
