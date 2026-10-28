import pytest

from matching.scorer import DEFAULT_WEIGHTS, WEIGHTS, load_weights, score
from schemas import History, Person, ScoreRequest


def req(a=None, b=None, h=None) -> ScoreRequest:
    return ScoreRequest(a=a or Person(), b=b or Person(), history=h or History())


def test_weights_are_documented_and_sum_to_one():
    assert WEIGHTS == {"interests": 0.30, "age": 0.15, "lifestyle": 0.30, "schedule": 0.10, "conversation": 0.15}
    assert sum(WEIGHTS.values()) == pytest.approx(1.0)


def test_identical_strong_profiles_score_very_high():
    p = Person(tags=["hiking", "coffee", "jazz"], age=30, active_hours=[18, 19, 20, 21],
               lifestyle={"goal": "long_term", "kids": "want", "smoking": "never", "drinking": "social", "exercise": "often"})
    r = score(req(p, p, History(a_to_b=15, b_to_a=15)))
    assert r.score >= 95
    assert [x.code for x in r.reasons][0] in ("shared_interests", "same_goals")
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


def life(**kw) -> Person:
    return Person(lifestyle=kw)


def test_wanting_the_same_thing_matters_and_is_named():
    same = score(req(life(goal="long_term"), life(goal="long_term")))
    clash = score(req(life(goal="long_term"), life(goal="casual")))
    assert same.components["lifestyle"] == pytest.approx(1.0)
    assert clash.components["lifestyle"] < 0.2
    assert same.score - clash.score >= 20
    assert [r.code for r in same.reasons] == ["same_goals"] and "long-term" in same.reasons[0].text
    assert not any(r.code == "same_goals" for r in clash.reasons)


def test_kids_dealbreaker_and_levels():
    assert score(req(life(kids="want"), life(kids="dont_want"))).components["lifestyle"] == 0.0
    assert score(req(life(kids="open"), life(kids="want"))).components["lifestyle"] > 0.7
    near = score(req(life(smoking="never"), life(smoking="social"))).components["lifestyle"]
    far = score(req(life(smoking="never"), life(smoking="often"))).components["lifestyle"]
    assert far == 0.0 and near == pytest.approx(0.5)


def test_missing_lifestyle_is_neutral_and_never_a_reason():
    r = score(req(life(goal="long_term"), Person()))
    assert r.components["lifestyle"] == 0.5
    assert not any(x.code in ("same_goals", "lifestyle_match") for x in r.reasons)


def test_a_broad_match_gets_a_lifestyle_reason_and_junk_is_ignored():
    a = life(goal="figuring_out", kids="open", smoking="never", drinking="social", exercise="often")
    r = score(req(a, a))
    assert any(x.code == "lifestyle_match" for x in r.reasons)
    junk = Person(lifestyle={"goal": "world_domination", "favourite_colour": "red", "kids": "want"})
    assert junk.lifestyle == {"kids": "want"}


def test_weights_can_be_set_from_a_setting_but_only_when_complete_and_summing_to_one():
    good = '{"interests": 0.4, "age": 0.1, "lifestyle": 0.3, "schedule": 0.1, "conversation": 0.1}'
    assert load_weights(good)["interests"] == 0.4
    assert load_weights(None) == DEFAULT_WEIGHTS == load_weights("")
    for bad in ["not json", "[]", '{"interests": 1}',
                '{"interests": 0.5, "age": 0.5, "lifestyle": 0.5, "schedule": 0, "conversation": 0}',
                '{"interests": -0.1, "age": 0.4, "lifestyle": 0.4, "schedule": 0.2, "conversation": 0.1}',
                '{"interests": "a", "age": 0.25, "lifestyle": 0.25, "schedule": 0.25, "conversation": 0.25}',
                '{"interests": 0.2, "age": 0.2, "lifestyle": 0.2, "schedule": 0.2, "conversation": 0.2, "extra": 0}']:
        assert load_weights(bad) == DEFAULT_WEIGHTS, bad
