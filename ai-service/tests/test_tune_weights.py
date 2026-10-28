import json
import random

import pytest

from tools import tune_weights as tw


def test_auc_basics():
    assert tw.auc([0.9, 0.8, 0.2, 0.1], [True, True, False, False]) == 1.0
    assert tw.auc([0.1, 0.2, 0.8, 0.9], [True, True, False, False]) == 0.0
    assert tw.auc([0.5, 0.5, 0.5, 0.5], [True, False, True, False]) == 0.5
    assert tw.auc([0.5, 0.6], [True, True]) == 0.5  # no bad outcomes to compare with


def test_search_prefers_the_component_that_predicts_the_outcome(tmp_path):
    rnd = random.Random(7)
    lines = []
    for _ in range(300):
        shared = rnd.random() < 0.5
        tags_a = ["a", "b", "c"]
        tags_b = ["a", "b", "c"] if shared else ["x", "y", "z"]
        # Only interests decide the outcome here; age is noise.
        lines.append(json.dumps({"a": {"tags": tags_a, "age": rnd.randint(20, 40)}, "b": {"tags": tags_b, "age": rnd.randint(20, 40)},
                                 "history": {}, "good": shared}))
    p = tmp_path / "m.jsonl"
    p.write_text("\n".join(lines) + "\nnot json\n", encoding="utf8")
    rows, good = tw.load(str(p))
    assert len(rows) == 300  # the bad line is skipped, not fatal
    assert tw.auc([r["interests"] for r in rows], good) > 0.95
    assert tw.auc([r["age"] for r in rows], good) < 0.6
    best, best_auc = tw.search(rows, good, 5)
    current = tw.auc(tw.blend(rows, tw.scorer.DEFAULT_WEIGHTS), good)
    assert best_auc >= current and best_auc >= 0.99
    assert sum(best.values()) == pytest.approx(1.0)
