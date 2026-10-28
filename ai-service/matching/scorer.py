"""Compatibility scoring.

score = 100 * (0.30*interests + 0.15*age + 0.30*lifestyle + 0.10*schedule + 0.15*conversation)

"lifestyle" is what someone wants and how they live: relationship goal, kids,
smoking, drinking and exercise. It replaced part of the interests and schedule
shares of the original 40/20/20/20 split because wanting the same thing out of
dating predicts a good match far better than overlapping active hours (which we
rarely know) and it is information members give us directly.

Each component is in [0, 1]. When a signal is missing (no tags, no age, no
history) it contributes a *neutral* value and is never offered as a "reason",
so sparse profiles are not punished and we never claim something we do not
know. Reasons are the top three components by weighted contribution among
those backed by real data.
"""
from __future__ import annotations

import json
import logging
import os

from schemas import History, Person, Reason, ScoreRequest, ScoreResponse

log = logging.getLogger(__name__)

DEFAULT_WEIGHTS = {"interests": 0.30, "age": 0.15, "lifestyle": 0.30, "schedule": 0.10, "conversation": 0.15}


def load_weights(raw: str | None) -> dict[str, float]:
    """Weights from the SCORE_WEIGHTS setting (JSON, e.g. {"interests": 0.4, ...}),
    so they can be tuned from real outcomes without a code change (see
    tools/tune_weights.py). Anything that is not a complete, non-negative set
    that adds up to 1 is ignored and the defaults are used: a typo in a setting
    must never silently change who is shown as a good match."""
    if not raw:
        return dict(DEFAULT_WEIGHTS)
    try:
        w = json.loads(raw)
        ok = (isinstance(w, dict) and set(w) == set(DEFAULT_WEIGHTS)
              and all(isinstance(v, (int, float)) and not isinstance(v, bool) and v >= 0 for v in w.values())
              and abs(sum(w.values()) - 1.0) < 1e-6)
    except ValueError:
        ok = False
    if not ok:
        log.warning("SCORE_WEIGHTS is not a complete set of non-negative weights that sum to 1; using the defaults")
        return dict(DEFAULT_WEIGHTS)
    return {k: float(w[k]) for k in DEFAULT_WEIGHTS}


WEIGHTS = load_weights(os.environ.get("SCORE_WEIGHTS"))

# Within lifestyle, how much each answer counts when both people gave it.
LIFESTYLE_WEIGHTS = {"goal": 0.40, "kids": 0.25, "smoking": 0.15, "drinking": 0.10, "exercise": 0.10}

# 1 = same aim, 0 = opposite aim. Symmetric; unlisted pairs are 0.4.
_GOAL_FIT = {
    frozenset({"long_term"}): 1.0, frozenset({"short_term"}): 1.0, frozenset({"casual"}): 1.0, frozenset({"friends"}): 1.0,
    frozenset({"figuring_out"}): 0.8,
    frozenset({"long_term", "short_term"}): 0.6, frozenset({"short_term", "casual"}): 0.7, frozenset({"casual", "friends"}): 0.5,
    frozenset({"long_term", "casual"}): 0.1, frozenset({"long_term", "friends"}): 0.2, frozenset({"short_term", "friends"}): 0.4,
}
_KIDS_FIT = {
    frozenset({"want"}): 1.0, frozenset({"dont_want"}): 1.0, frozenset({"open"}): 1.0, frozenset({"have"}): 1.0, frozenset({"unsure"}): 0.7,
    frozenset({"want", "dont_want"}): 0.0, frozenset({"want", "open"}): 0.8, frozenset({"dont_want", "open"}): 0.6,
    frozenset({"want", "have"}): 0.8, frozenset({"dont_want", "have"}): 0.2, frozenset({"open", "have"}): 0.9,
}
_LEVEL = {"never": 0, "sometimes": 1, "social": 1, "often": 2}
GOAL_TEXT = {"long_term": "a long-term relationship", "short_term": "something short-term", "casual": "something casual",
             "friends": "new friends", "figuring_out": "to see where things go"}
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


def lifestyle(a: Person, b: Person) -> tuple[float, int]:
    """Agreement on goal, kids, smoking, drinking and exercise, weighted, over
    the answers both people gave. Returns (score, answers compared)."""
    total = weight = 0.0
    compared = 0
    for key, w in LIFESTYLE_WEIGHTS.items():
        x, y = a.lifestyle.get(key), b.lifestyle.get(key)
        if x is None or y is None:
            continue
        if key == "goal":
            fit = _GOAL_FIT.get(frozenset({x, y}), 0.4)
        elif key == "kids":
            fit = _KIDS_FIT.get(frozenset({x, y}), 0.5)
        else:
            fit = 1.0 - abs(_LEVEL[x] - _LEVEL[y]) / 2
        total += w * fit
        weight += w
        compared += 1
    if compared == 0:
        return NEUTRAL, 0
    return total / weight, compared


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
    lf, lf_known = lifestyle(req.a, req.b)
    sc, overlap_hours = schedule(req.a, req.b)
    cv, msgs = conversation(req.history)

    comps = {"interests": i, "age": ag, "lifestyle": lf, "schedule": sc, "conversation": cv}
    total = sum(WEIGHTS[k] * v for k, v in comps.items())

    candidates: list[Reason] = []
    if shared:
        names = ", ".join(shared[:3])
        candidates.append(Reason(code="shared_interests", text=f"You both like {names}", weight=WEIGHTS["interests"] * i))
    ga, gb = req.a.lifestyle.get("goal"), req.b.lifestyle.get("goal")
    if ga and ga == gb and ga != "figuring_out":
        candidates.append(Reason(code="same_goals", text=f"You are both looking for {GOAL_TEXT[ga]}", weight=WEIGHTS["lifestyle"] * lf))
    elif lf_known >= 3 and lf >= 0.8:
        candidates.append(Reason(code="lifestyle_match", text="Your lifestyles line up", weight=WEIGHTS["lifestyle"] * lf))
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
