"""Check and tune the compatibility weights against what really happened.

The score is only worth having if higher scores go with better outcomes. This
tool takes a file of past matches and tells you, per component, how well it
separates the good outcomes from the bad (AUC: 0.5 is a coin flip, 1.0 is
perfect), then searches for weights that do better than the current ones.

Input: one JSON object per line,
  {"a": {...Person}, "b": {...Person}, "history": {...History}, "good": true}
where the Person/History shapes are exactly those of POST /score, and "good" is
your definition of a good outcome (for example: they exchanged at least ten
messages, or met, and did not unmatch or report). Export it from your own data;
never include names, photos or message text, only the fields the scorer reads.

  python tools/tune_weights.py matches.jsonl            # report and suggestion
  python tools/tune_weights.py matches.jsonl --steps 10 # finer search

The result is a SCORE_WEIGHTS value to try; it does not change anything. Do not
adopt it on a few hundred matches: with little data the "best" weights are
mostly noise. The tool says so when there are fewer than 500 rows.
"""
from __future__ import annotations

import argparse
import itertools
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from matching import scorer  # noqa: E402
from schemas import ScoreRequest  # noqa: E402

KEYS = list(scorer.DEFAULT_WEIGHTS)
MIN_ROWS = 500


def auc(scores: list[float], good: list[bool]) -> float:
    """Probability a random good outcome scores above a random bad one (ties
    count half). Returns 0.5 when one of the classes is empty."""
    pos = [s for s, g in zip(scores, good) if g]
    neg = [s for s, g in zip(scores, good) if not g]
    if not pos or not neg:
        return 0.5
    ranked = sorted(range(len(scores)), key=lambda i: scores[i])
    ranks = [0.0] * len(scores)
    i = 0
    while i < len(ranked):
        j = i
        while j + 1 < len(ranked) and scores[ranked[j + 1]] == scores[ranked[i]]:
            j += 1
        for k in range(i, j + 1):
            ranks[ranked[k]] = (i + j) / 2 + 1
        i = j + 1
    rank_sum = sum(r for r, g in zip(ranks, good) if g)
    return (rank_sum - len(pos) * (len(pos) + 1) / 2) / (len(pos) * len(neg))


def components(req: ScoreRequest) -> dict[str, float]:
    return {
        "interests": scorer.interests(req.a, req.b)[0],
        "age": scorer.age(req.a, req.b)[0],
        "lifestyle": scorer.lifestyle(req.a, req.b)[0],
        "schedule": scorer.schedule(req.a, req.b)[0],
        "conversation": scorer.conversation(req.history)[0],
    }


def load(path: str) -> tuple[list[dict[str, float]], list[bool]]:
    rows, good = [], []
    for n, line in enumerate(Path(path).read_text(encoding="utf8").splitlines(), 1):
        if not line.strip():
            continue
        try:
            obj = json.loads(line)
            flag = obj.pop("good")
            rows.append(components(ScoreRequest(**obj)))
            good.append(bool(flag))
        except Exception as e:  # noqa: BLE001 - say which line is bad, keep going
            print(f"line {n}: skipped ({e})", file=sys.stderr)
    return rows, good


def blend(rows: list[dict[str, float]], w: dict[str, float]) -> list[float]:
    return [sum(w[k] * r[k] for k in KEYS) for r in rows]


def search(rows: list[dict[str, float]], good: list[bool], steps: int) -> tuple[dict[str, float], float]:
    best, best_auc = dict(scorer.DEFAULT_WEIGHTS), auc(blend(rows, scorer.DEFAULT_WEIGHTS), good)
    for combo in itertools.product(range(steps + 1), repeat=len(KEYS) - 1):
        if sum(combo) > steps:
            continue
        parts = list(combo) + [steps - sum(combo)]
        w = {k: p / steps for k, p in zip(KEYS, parts)}
        a = auc(blend(rows, w), good)
        if a > best_auc + 1e-9:
            best, best_auc = w, a
    return best, best_auc


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("file")
    ap.add_argument("--steps", type=int, default=10, help="weight grid: multiples of 1/steps (default 10)")
    args = ap.parse_args()
    rows, good = load(args.file)
    if not rows:
        print("no usable rows")
        return 1
    print(f"{len(rows)} matches, {sum(good)} with a good outcome ({100 * sum(good) / len(rows):.0f}%)\n")
    print("How well each part alone separates good from bad (0.5 = no better than chance):")
    for k in KEYS:
        print(f"  {k:<13} AUC {auc([r[k] for r in rows], good):.3f}   current weight {scorer.WEIGHTS[k]:.2f}")
    cur = auc(blend(rows, scorer.WEIGHTS), good)
    print(f"\nCurrent blend      AUC {cur:.3f}")
    best, best_auc = search(rows, good, args.steps)
    print(f"Best found         AUC {best_auc:.3f}")
    print("\nTry:  SCORE_WEIGHTS='" + json.dumps({k: round(v, 3) for k, v in best.items()}) + "'")
    if len(rows) < MIN_ROWS:
        print(f"\nOnly {len(rows)} rows (fewer than {MIN_ROWS}): treat the suggestion as noise, not evidence.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
