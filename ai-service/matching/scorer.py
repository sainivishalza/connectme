"""Compatibility scoring.

score = 100 * (0.40*interests + 0.20*age + 0.20*schedule + 0.20*conversation)

Each component is in [0, 1]. When a signal is missing (no tags, no age, no
history) it contributes a *neutral* value and is never offered as a "reason",
so sparse profiles are not punished and we never claim something we do not
know. Reasons are the top three components by weighted contribution among
those backed by real data.
"""
from __future__ import annotations

from schemas import History, Person, Reason, ScoreRequest, ScoreResponse

WEIGHTS = {"interests": 0.40, "age": 0.20, "schedule": 0.20, "conversation": 0.20}
NEUTRAL = 0.5
AGE_FALLOFF_YEARS = 10.0
HISTORY_SATURATION = 20  # messages after which volume stops adding score


def interests(a: Person, b: Person) -> tuple[float, list[str]]:
    """Sørensen-Dice overlap of interest tags, with a bonus for 3+ shared."""
    if not a.tags or not b.tags:
        return NEUTRAL, []
    sa, sb = set(a.tags), set(b.tags)
    shared = sorted(sa & sb)
    dice = 2 * len(shared) / (len(sa) + len(sb))
    # Dice rarely reaches 1 for real profiles; stretch so 3+ shared is strong.
    bonus = min(len(shared), 3) / 3 * 0.25
    return min(1.0, dice + bonus), shared


def age(a: Person, b: Person) -> tuple[float, int | None]:
    if a.age is None or b.age is None:
        return NEUTRAL, None
    diff = abs(a.age - b.age)
    return max(0.0, 1.0 - diff / AGE_FALLOFF_YEARS), diff


def schedule(a: Person, b: Person) -> tuple[float, int]:
    """Overlap of typical active hours (Jaccard over the 24-hour clock)."""
    if not a.active_hours or not b.active_hours:
        return NEUTRAL, 0
    sa, sb = set(a.active_hours), set(b.active_hours)
    both = len(sa & sb)
    return both / len(sa | sb), both


def conversation(h: History) -> tuple[float, int]:
    total = h.a_to_b + h.b_to_a
    if total == 0:
        return NEUTRAL, 0
    reciprocity = min(h.a_to_b, h.b_to_a) / max(h.a_to_b, h.b_to_a)
    volume = min(1.0, total / HISTORY_SATURATION)
    return 0.5 * reciprocity + 0.5 * volume, total


def score(req: ScoreRequest) -> ScoreResponse:
    i, shared = interests(req.a, req.b)
    ag, diff = age(req.a, req.b)
    sc, overlap_hours = schedule(req.a, req.b)
    cv, msgs = conversation(req.history)

    comps = {"interests": i, "age": ag, "schedule": sc, "conversation": cv}
    total = sum(WEIGHTS[k] * v for k, v in comps.items())

    candidates: list[Reason] = []
    if shared:
        names = ", ".join(shared[:3])
        candidates.append(Reason(code="shared_interests", text=f"You both like {names}", weight=WEIGHTS["interests"] * i))
    if diff is not None and diff <= 5:
        text = "You are the same age" if diff == 0 else f"Only {diff} year{'s' if diff != 1 else ''} apart"
        candidates.append(Reason(code="similar_age", text=text, weight=WEIGHTS["age"] * ag))
    if overlap_hours >= 3:
        candidates.append(Reason(code="similar_schedule", text="You are usually online at the same times", weight=WEIGHTS["schedule"] * sc))
    if msgs >= 4 and cv >= 0.5:
        candidates.append(Reason(code="active_conversation", text="You are both keeping the conversation going", weight=WEIGHTS["conversation"] * cv))

    candidates.sort(key=lambda r: r.weight, reverse=True)
    return ScoreResponse(
        score=max(0, min(100, round(total * 100))),
        reasons=candidates[:3],
        components={k: round(v, 3) for k, v in comps.items()},
    )
