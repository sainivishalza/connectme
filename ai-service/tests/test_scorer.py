import pytest

from matching.scorer import WEIGHTS, score
from schemas import History, Person, ScoreRequest


def req(a=None, b=None, h=None) -> ScoreRequest:
    return ScoreRequest(a=a or Person(), b=b or Person(), history=h or History())


def test_weights_match_the_spec():
    assert WEIGHTS == {"interests": 0.40, "age": 0.20, "schedule": 0.20, "conversation": 0.20}
    assert sum(WEIGHTS.values()) == pytest.approx(1.0)


def test_identical_strong_profiles_score_very_high():
    p = Person(tags=["hiking", "coffee", "jazz"], age=30, active_hours=[18, 19, 20, 21])
    r = score(req(p, p, History(a_to_b=15, b_to_a=15)))
    assert r.score >= 95
    assert [x.code for x in r.reasons][0] == "shared_interests"
    assert len(r.reasons) <= 3


def test_unknown_signals_are_neutral_not_punished_and_not_claimed():
    r = score(req())
    assert r.score == 50  # every component neutral
    assert r.reasons == []


def test_no_overlap_scores_low():
    a = Person(tags=["a", "b"], age=20, active_hours=[1, 2, 3])
    b = Person(tags=["c", "d"], age=45, active_hours=[14, 15, 16])
    r = score(req(a, b, History(a_to_b=10, b_to_a=0)))
    assert r.score < 25
    assert r.reasons == []


@pytest.mark.parametrize("diff,expected", [(0, 1.0), (5, 0.5), (10, 0.0), (30, 0.0)])
def test_age_component(diff, expected):
    r = score(req(Person(age=30), Person(age=30 + diff)))
    assert r.components["age"] == pytest.approx(expected)


def test_more_shared_interests_never_lowers_score():
    base = Person(tags=["x", "y", "z", "w"])
    last = -1
    for n in range(0, 5):
        other = Person(tags=["x", "y", "z", "w"][:n] + [f"o{i}" for i in range(4 - n)])
        s = score(req(base, other)).score
        assert s >= last
        last = s


def test_one_sided_conversation_scores_lower_than_reciprocal():
    one = score(req(h=History(a_to_b=20, b_to_a=0))).components["conversation"]
    both = score(req(h=History(a_to_b=10, b_to_a=10))).components["conversation"]
    assert both > one


def test_reasons_never_exceed_three_and_are_sorted():
    p = Person(tags=["a", "b", "c"], age=28, active_hours=list(range(8, 20)))
    r = score(req(p, p, History(a_to_b=10, b_to_a=9)))
    assert len(r.reasons) == 3
    weights = [x.weight for x in r.reasons]
    assert weights == sorted(weights, reverse=True)


def test_score_is_always_in_range_and_deterministic():
    p = Person(tags=["a"] * 5, age=18, active_hours=[0, 23])
    r1, r2 = score(req(p, p)), score(req(p, p))
    assert 0 <= r1.score <= 100
    assert r1 == r2


def test_input_validation_normalises_tags_and_hours():
    p = Person(tags=[" Hiking ", "hiking", "", "COFFEE"], active_hours=[25, -1, 3, 3])
    assert p.tags == ["hiking", "coffee"]
    assert p.active_hours == [3]
