// Package games holds the social games: truth, dare and icebreaker prompts and
// the two-person couple quiz.
//
// Content rules for every prompt: suitable for adults who may have just
// matched, never sexual, never asking for money, an address, a workplace, a
// full name, photos or anything that identifies someone, never involving
// alcohol, risk or other people's property. A dare is something that can be
// done from a chair in a minute.
package games

// Prompt kinds.
const (
	KindTruth      = "truth"
	KindDare       = "dare"
	KindIcebreaker = "icebreaker"
)

var truths = []string{
	"What is a small thing that always makes your day better?",
	"What is the most embarrassing song you secretly love?",
	"What is something you are surprisingly good at?",
	"What was your most memorable trip, and why?",
	"What is a habit of yours that you think is a bit odd?",
	"When did you last laugh so hard you cried?",
	"What is a skill you wish you could learn in a weekend?",
	"What did you want to be when you were ten?",
	"What is the best meal you have ever had?",
	"What is a movie you can watch over and over?",
	"What is the nicest thing a stranger has done for you?",
	"What is a food you pretend to like?",
	"What is your most unpopular harmless opinion?",
	"What was your favourite subject at school, and what was the worst?",
	"If you could live in any decade, which one and why?",
	"What is something you are proud of that nobody asks about?",
	"What is the last thing that made you really curious?",
	"What does a perfect lazy Sunday look like for you?",
	"What is a tradition from your family that you love?",
	"What is the best advice you have ever been given?",
	"What is a place you could happily go back to every year?",
	"What is something you have changed your mind about?",
	"What do you do when you cannot sleep?",
	"What is a compliment you still remember?",
}

var dares = []string{
	"Describe your day using only three emojis, then explain them.",
	"Send a voice note singing the first line of the last song you listened to.",
	"Write a two-line poem about the nearest object to you.",
	"Tell a joke in the worst accent you can do (in text, describe the accent).",
	"Invent a new word and give it a meaning. Use it in a sentence.",
	"Give yourself a movie-trailer voiceover line for your week.",
	"Describe your dream sandwich in as much detail as you can.",
	"Pick a colour and explain why it is secretly the best one.",
	"Tell a very short story using exactly ten words.",
	"Rate your last meal like a food critic, in one dramatic sentence.",
	"Describe your phone's home screen as if it were a museum exhibit.",
	"Say a tongue twister three times and report how it went.",
	"Come up with a superhero name for yourself and a weakness for it.",
	"Describe the last dream you remember in one sentence.",
	"Choose an animal that matches your mood today and defend the choice.",
	"Invent a holiday and say how it should be celebrated.",
	"Write the title of your autobiography and its subtitle.",
	"Explain your job (or study) to a five-year-old in two sentences.",
	"Name three things you can see right now and make them into a story.",
	"Give your best fake weather forecast for tomorrow's mood.",
	"Hum a tune and let the other person guess a genre (in text, describe it).",
	"Draw something simple with your finger in the air and describe what it is.",
	"Pitch a terrible startup idea with total confidence.",
	"Recommend one song, one snack and one film for the perfect evening.",
}

var icebreakers = []string{
	"Coffee, tea or something else entirely?",
	"Mountains, beach or city break?",
	"What is the best thing you have watched or read lately?",
	"Early bird or night owl?",
	"What is your go-to comfort food?",
	"What is one thing on your bucket list?",
	"What are you looking forward to this week?",
	"If you could have dinner with anyone, who would it be?",
	"Cats, dogs or neither?",
	"What is a hobby you would love to spend more time on?",
	"What is the best live event you have ever been to?",
	"Sweet or savoury?",
	"What song is on repeat for you right now?",
	"What is your favourite way to spend a rainy day?",
	"What is something small you are grateful for today?",
	"What is the most interesting thing you learned this year?",
	"Do you plan trips in detail or just go?",
	"What is a place near you that you would show a visitor?",
	"What is your favourite season, and what do you do in it?",
	"What would your ideal first meeting look like?",
	"Which would you rather: a quiet night in or a spontaneous night out?",
	"What is something you cook (or want to learn to cook)?",
	"What was the last thing that made you smile?",
	"What is your favourite thing about where you live?",
}

// Question is one quiz question.
type Question struct {
	Text    string   `json:"text"`
	Options []string `json:"options"`
}

// QuizSet is a named list of questions both people answer.
type QuizSet struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Questions []Question `json:"-"`
}

// Quizzes is the bank of couple quizzes.
var Quizzes = []QuizSet{
	{ID: "everyday", Title: "Everyday life", Questions: []Question{
		{"Pick a morning:", []string{"Slow with coffee", "Up and out early", "Hit snooze, then rush"}},
		{"A free evening is best spent:", []string{"At home with a film", "Out with friends", "Trying something new", "Cooking something great"}},
		{"Your phone is usually:", []string{"On silent", "Always buzzing", "Somewhere I cannot find it"}},
		{"Your kitchen style:", []string{"Recipes, followed exactly", "Improvise", "Order in"}},
		{"Messages are best:", []string{"Quick and often", "Longer, less often", "Voice notes", "Save it for a call"}},
		{"Planning a day off:", []string{"Hour by hour", "A rough idea", "No plan at all"}},
		{"Your space is:", []string{"Tidy", "Organised chaos", "Lived in"}},
		{"A good surprise is:", []string{"A small gift", "A planned outing", "Time together, no agenda"}},
	}},
	{ID: "dating", Title: "How we date", Questions: []Question{
		{"A great first date is:", []string{"Coffee and a walk", "Dinner somewhere nice", "An activity", "Somewhere lively"}},
		{"Texting between dates:", []string{"Every day", "A few times a week", "Only to make plans"}},
		{"Moving at the right speed means:", []string{"Slow and steady", "Whatever feels natural", "Quick when it is right"}},
		{"Your love of plans:", []string{"Planner", "Go with the flow", "Depends on the day"}},
		{"Spending time together:", []string{"Most days", "A few times a week", "Weekends"}},
		{"Tough conversations should happen:", []string{"Right away", "After a calm moment", "Face to face only"}},
		{"The best compliment is about:", []string{"Humour", "Kindness", "Looks", "Ambition"}},
		{"A deal-breaker is:", []string{"Dishonesty", "Rudeness to others", "Being always late", "No sense of humour"}},
	}},
	{ID: "weekend", Title: "Weekend vibes", Questions: []Question{
		{"Saturday morning:", []string{"Brunch", "Gym or a run", "Sleep in", "Market or errands"}},
		{"A road trip needs:", []string{"A great playlist", "Snacks", "A destination", "Good company"}},
		{"Your ideal holiday:", []string{"Beach", "Mountains", "City", "Countryside"}},
		{"Music at home:", []string{"Always on", "Sometimes", "Prefer quiet"}},
		{"Games night means:", []string{"Board games", "Video games", "Cards", "Not really my thing"}},
		{"For dinner out you choose:", []string{"Somewhere familiar", "Something I have never tried", "Whatever is closest"}},
		{"Sunday evening is for:", []string{"Planning the week", "A long bath or walk", "Family or friends", "A film"}},
		{"A perfect view is:", []string{"Sunrise", "Sunset", "Stars", "City lights"}},
	}},
}

// QuizByID finds a quiz set.
func QuizByID(id string) (QuizSet, bool) {
	for _, q := range Quizzes {
		if q.ID == id {
			return q, true
		}
	}
	return QuizSet{}, false
}
