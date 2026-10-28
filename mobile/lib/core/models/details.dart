import 'json.dart';

/// What someone wants and how they live, plus (on full profiles) a few
/// identifying facts. The server decides which of these a viewer receives.
class ProfileDetails {
  const ProfileDetails({
    this.heightCm = 0,
    this.jobTitle = '',
    this.education = '',
    this.hometown = '',
    this.zodiac = '',
    this.goal = '',
    this.kids = '',
    this.drinking = '',
    this.smoking = '',
    this.exercise = '',
    this.pets = '',
    this.languages = const [],
  });

  final int heightCm;
  final String jobTitle;
  final String education;
  final String hometown;
  final String zodiac;
  final String goal;
  final String kids;
  final String drinking;
  final String smoking;
  final String exercise;
  final String pets;
  final List<String> languages;

  factory ProfileDetails.fromJson(Map<String, dynamic> j) => ProfileDetails(
        heightCm: jsonInt(j['height_cm']),
        jobTitle: jsonString(j['job_title']),
        education: jsonString(j['education']),
        hometown: jsonString(j['hometown']),
        zodiac: jsonString(j['zodiac']),
        goal: jsonString(j['goal']),
        kids: jsonString(j['kids']),
        drinking: jsonString(j['drinking']),
        smoking: jsonString(j['smoking']),
        exercise: jsonString(j['exercise']),
        pets: jsonString(j['pets']),
        languages: jsonStrings(j['languages']),
      );

  /// Short facts for the chips on a card, in the order people care about.
  List<DetailFact> get facts => [
        if (goal.isNotEmpty) DetailFact(DetailIcon.goal, DetailLabels.goal[goal] ?? goal),
        if (heightCm > 0) DetailFact(DetailIcon.height, '$heightCm cm'),
        if (jobTitle.isNotEmpty) DetailFact(DetailIcon.job, jobTitle),
        if (education.isNotEmpty) DetailFact(DetailIcon.education, DetailLabels.education[education] ?? education),
        if (hometown.isNotEmpty) DetailFact(DetailIcon.hometown, hometown),
        if (kids.isNotEmpty) DetailFact(DetailIcon.kids, DetailLabels.kids[kids] ?? kids),
        if (drinking.isNotEmpty) DetailFact(DetailIcon.drinking, 'Drinks: ${DetailLabels.frequency[drinking] ?? drinking}'),
        if (smoking.isNotEmpty) DetailFact(DetailIcon.smoking, 'Smokes: ${DetailLabels.frequency[smoking] ?? smoking}'),
        if (exercise.isNotEmpty) DetailFact(DetailIcon.exercise, 'Exercise: ${DetailLabels.exercise[exercise] ?? exercise}'),
        if (pets.isNotEmpty) DetailFact(DetailIcon.pets, DetailLabels.pets[pets] ?? pets),
        if (languages.isNotEmpty) DetailFact(DetailIcon.languages, languages.map((l) => DetailLabels.languages[l] ?? l).join(', ')),
        if (zodiac.isNotEmpty) DetailFact(DetailIcon.zodiac, DetailLabels.zodiac[zodiac] ?? zodiac),
      ];
}

enum DetailIcon { goal, height, job, education, hometown, kids, drinking, smoking, exercise, pets, languages, zodiac }

class DetailFact {
  const DetailFact(this.icon, this.text);
  final DetailIcon icon;
  final String text;
}

/// Wording for the stored option keys. The server only ever sees the keys.
abstract final class DetailLabels {
  static const goal = {
    'long_term': 'Long-term relationship',
    'short_term': 'Something short-term',
    'casual': 'Something casual',
    'friends': 'New friends',
    'figuring_out': 'Still figuring it out',
  };
  static const kids = {
    'want': 'Wants kids',
    'dont_want': "Doesn't want kids",
    'open': 'Open to kids',
    'have': 'Has kids',
    'unsure': 'Unsure about kids',
  };
  static const frequency = {'never': 'Never', 'social': 'Socially', 'often': 'Often'};
  static const exercise = {'never': 'Never', 'sometimes': 'Sometimes', 'often': 'Often'};
  static const pets = {'dog': 'Dog person', 'cat': 'Cat person', 'both': 'Dogs and cats', 'none': 'No pets', 'other': 'Other pets'};
  static const education = {
    'high_school': 'High school',
    'trade': 'Trade school',
    'college': 'College',
    'bachelor': "Bachelor's degree",
    'master': "Master's degree",
    'doctorate': 'Doctorate',
    'other': 'Other',
  };
  static const zodiac = {
    'aries': 'Aries', 'taurus': 'Taurus', 'gemini': 'Gemini', 'cancer': 'Cancer', 'leo': 'Leo', 'virgo': 'Virgo',
    'libra': 'Libra', 'scorpio': 'Scorpio', 'sagittarius': 'Sagittarius', 'capricorn': 'Capricorn', 'aquarius': 'Aquarius', 'pisces': 'Pisces',
  };
  static const languages = {
    'en': 'English', 'es': 'Spanish', 'fr': 'French', 'de': 'German', 'it': 'Italian', 'pt': 'Portuguese', 'nl': 'Dutch',
    'sv': 'Swedish', 'pl': 'Polish', 'ru': 'Russian', 'tr': 'Turkish', 'ar': 'Arabic', 'hi': 'Hindi', 'bn': 'Bengali',
    'ur': 'Urdu', 'zh': 'Chinese', 'ja': 'Japanese', 'ko': 'Korean', 'th': 'Thai', 'vi': 'Vietnamese', 'id': 'Indonesian',
    'el': 'Greek', 'he': 'Hebrew', 'uk': 'Ukrainian', 'ro': 'Romanian', 'cs': 'Czech', 'hu': 'Hungarian', 'fi': 'Finnish',
    'da': 'Danish', 'no': 'Norwegian',
  };
}

/// One answered prompt ("My perfect Sunday is" / "Market, long lunch").
class PromptAnswer {
  const PromptAnswer({required this.key, required this.prompt, required this.answer});
  final String key;
  final String prompt;
  final String answer;

  factory PromptAnswer.fromJson(Map<String, dynamic> j) =>
      PromptAnswer(key: jsonString(j['key']), prompt: jsonString(j['prompt']), answer: jsonString(j['answer']));
}

/// A question a member can answer.
class PromptDef {
  const PromptDef(this.key, this.text);
  final String key;
  final String text;

  factory PromptDef.fromJson(Map<String, dynamic> j) => PromptDef(jsonString(j['key']), jsonString(j['text']));
}

/// How finished the member's own profile is and what to add next.
class Completeness {
  const Completeness({this.percent = 0, this.nextSteps = const []});
  final int percent;
  final List<String> nextSteps;

  factory Completeness.fromJson(Map<String, dynamic> j) => Completeness(percent: jsonInt(j['percent']), nextSteps: jsonStrings(j['next_steps']));

  /// A short instruction for a step key from the server.
  static String stepText(String key) => switch (key) {
        'add_photo' => 'Add your first photo',
        'add_more_photos' => 'Add at least three photos',
        'write_intro' => 'Write a one-line intro',
        'write_bio' => 'Tell people a little about you',
        'pick_interests' => 'Pick three or more interests',
        'set_goal' => 'Say what you are looking for',
        'fill_lifestyle' => 'Fill in your lifestyle',
        'answer_prompts' => 'Answer two prompts',
        'share_location' => 'Share your location',
        'verify_id' => 'Verify with an ID',
        _ => key,
      };
}
