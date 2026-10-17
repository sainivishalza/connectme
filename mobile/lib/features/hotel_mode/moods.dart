/// Traveller moods a guest can pick for their stay. The keys are what the API
/// stores; the labels are what people see.
const travelMoods = <String, String>{
  'business': 'On a business trip',
  'company': 'Looking for company',
  'guide': 'Happy to show you around',
};

String moodLabel(String key) => travelMoods[key] ?? '';
