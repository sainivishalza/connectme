"""Icebreakers and date ideas.

Deterministic and template-based: the same inputs give the same output, it
needs no external service or API key, and it only ever sees the activity, the
group size and public interest tags. Venue ideas are *kinds* of places, not
named businesses, because the service is never given a location.

To use a language model instead, replace `suggest` behind the same
request/response schema; nothing else needs to change.
"""
from __future__ import annotations

import hashlib

from schemas import SuggestRequest, SuggestResponse

VENUES = {
    "dinner": [
        "A casual restaurant with shareable plates",
        "A rooftop or terrace dining spot",
        "A food hall with many options so everyone finds something",
        "A cosy neighbourhood bistro with a quiet corner",
        "A night market or street-food area",
    ],
    "hiking": [
        "A well-marked trail close to town",
        "A scenic viewpoint with an easy walk",
        "A park loop with a café at the end",
        "A waterfront or riverside path",
        "A botanical garden for a gentler outing",
    ],
    "concert": [
        "A small live-music venue",
        "An open-air show or festival",
        "A bar with a local band night",
        "A jazz or acoustic session",
        "A free community concert in a park",
    ],
    "bar_crawl": [
        "A cluster of bars within walking distance",
        "A cocktail bar followed by a relaxed pub",
        "A district known for its nightlife",
        "A wine bar with tasting flights",
        "A board-game bar to break the ice",
    ],
    "custom": [
        "A neutral public place both groups can reach easily",
        "A café with plenty of seating",
        "A popular public venue with good transport links",
        "An activity centre (bowling, mini-golf, climbing)",
        "A park with space to spread out",
    ],
}

GENERIC_ICEBREAKERS = [
    "What's the best trip any of you have taken together?",
    "Two truths and a lie, crew edition.",
    "What would your group's dream weekend look like?",
    "Which of you is most likely to get lost, and why?",
    "What's a small thing that always makes your day better?",
    "What's the most spontaneous thing you've done as a group?",
    "If you could only eat one cuisine for a year, what would you pick?",
]

ACTIVITY_ICEBREAKERS = {
    "dinner": "What's a dish you'd happily eat every week?",
    "hiking": "Mountains or coast, and what's your favourite view?",
    "concert": "What's the first gig you ever went to?",
    "bar_crawl": "What's your go-to drink order?",
}


def _pick(items: list[str], seed: str, n: int) -> list[str]:
    """Stable pseudo-random selection: same seed, same picks."""
    ranked = sorted(items, key=lambda s: hashlib.sha256((seed + s).encode()).hexdigest())
    return ranked[:n]


def suggest(req: SuggestRequest) -> SuggestResponse:
    activity = req.activity if req.activity in VENUES else "custom"
    seed = f"{activity}:{req.group_size}:{','.join(req.shared_tags)}"
    venues = _pick(VENUES[activity], seed, 3)

    ice: list[str] = [f"You both seem into {t}. What got you started?" for t in req.shared_tags[:3]]
    if activity in ACTIVITY_ICEBREAKERS:
        ice.append(ACTIVITY_ICEBREAKERS[activity])
    ice.extend(_pick(GENERIC_ICEBREAKERS, seed, 7))

    seen: set[str] = set()
    out = [s for s in ice if not (s in seen or seen.add(s))]
    if req.group_size >= 4:
        out.insert(1, "Pick a team name for your group.")
    return SuggestResponse(venues=venues, icebreakers=out[:7], source="ai")
