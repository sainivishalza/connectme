"""Rule-based text moderation.

This is a *first line* of defence: fast, deterministic, explainable, and it
never sends text anywhere. It is deliberately conservative about blocking and
prefers "review" for ambiguous cases. It cannot replace a trained classifier
or human moderators; production should add vendor word lists via
MODERATION_EXTRA_BLOCKLIST and a model behind the same interface.

Chats are end-to-end encrypted and are never scanned: this applies to public
text only (profile fields, hotel board posts, group names/descriptions).
"""
from __future__ import annotations

import os
import re
import unicodedata
from functools import lru_cache

from schemas import TextKind, Verdict

_ZERO_WIDTH = dict.fromkeys(map(ord, "​‌‍⁠﻿"), None)
_LEET = str.maketrans({"0": "o", "1": "i", "3": "e", "4": "a", "5": "s", "7": "t", "@": "a", "$": "s", "!": "i"})

# Mild profanity: "review" in free text, "block" in names and group titles.
PROFANITY = {"fuck", "fucking", "shit", "bitch", "asshole", "dick", "cunt", "bastard", "slut", "whore", "pussy"}

THREATS = [
    r"\bkill\s*your\s*self\b", r"\bkys\b", r"\bgo\s+die\b", r"\bi('?ll|\s+will)\s+(find|kill|hurt|rape|stab)\b",
    r"\bi\s+know\s+where\s+you\s+live\b", r"\byou\s+(deserve|should)\s+(to\s+)?die\b",
]

# Statements of being a minor. Numbers followed by time/distance units are excluded.
_NOT_UNITS = r"(?!\s*(?:min|mins|minutes?|km|kms|miles?|mi\b|hours?|hrs?|cm|kg|lbs?|%|m\b|metres?|meters?|years?\s+(?:ago|in)))"
UNDERAGE = [
    rf"\b(?:i\s*am|i'?m|im)\s*(?:only\s*|just\s*)?(1[0-7]){_NOT_UNITS}\b",
    r"\b(1[0-7])\s*(?:yo|y/o|yrs?\s*old|years?\s*old|year[- ]old)\b",
    r"\b(?:in|going\s+into)\s+(?:the\s+)?(?:6th|7th|8th|9th|10th|11th)\b",
    r"\bmiddle\s+school(?:er)?\b", r"\bi'?m\s+a\s+minor\b", r"\bunder\s*age\b",
]
UNDERAGE_REVIEW = [r"\bhigh\s+school\s+(?:student|freshman|sophomore|junior)\b", r"\bsweet\s+sixteen\b"]

COMMERCIAL_SEX = [
    r"\bescort\b", r"\bgfe\b", r"\bfull\s+service\b", r"\bhappy\s+ending\b", r"\bpaid\s+companion(?:ship)?\b",
    r"\$\s*\d+\s*(?:/|per)\s*(?:hour|hr|night|h)\b",
]

SCAM = [
    r"\bsend\s+(?:me\s+)?(?:some\s+)?(?:money|cash|btc|bitcoin|crypto|usdt)\b",
    r"\bgift\s*cards?\b", r"\bwire\s+transfer\b", r"\bwestern\s+union\b", r"\bmoneygram\b",
    r"\binvest(?:ing|ment)?\s+(?:in\s+)?(?:crypto|bitcoin|forex|stocks|binary)\b",
    r"\bdouble\s+your\s+(?:money|investment)\b", r"\bguaranteed\s+(?:returns?|profit)\b",
    r"\b(?:cash\s*app|venmo|paypal\.me|zelle)\b", r"\bi'?m\s+(?:stuck|stranded)\s+(?:abroad|overseas)\b",
]

OFF_PLATFORM = [r"\b(?:whatsapp|telegram|kik|wechat|snap(?:chat)?|instagram|insta)\b"]
LINKS = [r"https?://", r"\bwww\.", r"\b[a-z0-9-]+\.(?:com|net|org|io|me|ly|cc|xyz|ru)\b", r"\bt\.me/", r"\bbit\.ly/"]
PHONE = re.compile(r"(?<!\d)(?:\+?\d[\s().-]?){9,14}\d(?!\d)")
EMAIL = re.compile(r"[a-z0-9._%+-]+\s*(?:@|\(at\)|\[at\])\s*[a-z0-9.-]+\s*(?:\.|\(dot\)|\[dot\])\s*[a-z]{2,}", re.I)

_RANK = {"allow": 0, "review": 1, "block": 2}


def _norm(text: str) -> str:
    t = unicodedata.normalize("NFKD", text).translate(_ZERO_WIDTH)
    t = "".join(c for c in t if not unicodedata.combining(c)).lower()
    return re.sub(r"\s+", " ", t).strip()


def _leet(text: str) -> str:
    """Digits/symbols become letters only *inside* words, so "I'm 15" stays intact."""
    return re.sub(
        r"(?<=[a-z])[01345!@$7]+(?=[a-z])|(?<=[a-z])[01345!@$7](?=\b)",
        lambda m: m.group(0).translate(_LEET),
        text,
    )


def _squash(text: str) -> str:
    """Undo common evasions: f.u.c.k -> fuck, sh1t -> shit, fuuuck -> fuuck."""
    t = text
    if re.search(r"\b(?:\w[\s.\-_*]){3,}\w\b", t):
        t = re.sub(r"(?<=\w)[\s.\-_*]+(?=\w)", "", t)
    return re.sub(r"(.)\1{2,}", r"\1\1", _leet(t))


@lru_cache(maxsize=1)
def _extra_blocklist() -> frozenset[str]:
    path = os.environ.get("MODERATION_EXTRA_BLOCKLIST", "")
    if not path or not os.path.isfile(path):
        return frozenset()
    with open(path, encoding="utf-8") as f:
        return frozenset(w for w in (_norm(x) for x in f) if w and not w.startswith("#"))


def _any(patterns: list[str], text: str) -> bool:
    return any(re.search(p, text) for p in patterns)


def _words(text: str) -> set[str]:
    return set(re.findall(r"[a-z]+", text))


def _collapse(word: str) -> str:
    """Runs of one letter become one letter, on both sides of a comparison,
    so 'fuuuck' matches 'fuck' and 'asshole' still matches 'assshole'."""
    return re.sub(r"(.)\1+", r"\1", word)


def check_text(text: str, kind: TextKind = "bio") -> Verdict:
    raw = text or ""
    if not raw.strip():
        return Verdict(safe=True, action="allow", reason="empty", categories=[])

    n = _norm(raw)
    s = _squash(n)
    hits: list[tuple[str, str]] = []  # (category, action)

    # Age first: the most serious signal for a dating product.
    if _any(UNDERAGE, n) or _any(UNDERAGE, s):
        hits.append(("underage", "block"))
    elif _any(UNDERAGE_REVIEW, n):
        hits.append(("underage_signal", "review"))

    if _any(THREATS, n) or _any(THREATS, s):
        hits.append(("threat", "block"))
    if _any(COMMERCIAL_SEX, n):
        hits.append(("commercial_sex", "block"))
    if _any(SCAM, n):
        hits.append(("scam", "block"))

    if _any(OFF_PLATFORM, n):
        hits.append(("off_platform_contact", "review"))
    links = sum(len(re.findall(p, n)) for p in LINKS)
    if links >= 3:
        hits.append(("spam", "block"))
    elif links >= 1:
        hits.append(("link", "review"))

    if PHONE.search(raw) or EMAIL.search(raw):
        # Contact details in public text bypass in-app safety; a board post is seen by a whole venue.
        hits.append(("contact_info", "block" if kind == "post" else "review"))

    words = {_collapse(w) for w in _words(s) | _words(n)}
    if words & {_collapse(w) for w in PROFANITY}:
        hits.append(("profanity", "block" if kind in ("name", "group") else "review"))
    if words & {_collapse(w) for w in _extra_blocklist()}:
        hits.append(("blocklist", "block"))

    letters = [c for c in raw if c.isalpha()]
    if len(letters) >= 20 and sum(c.isupper() for c in letters) / len(letters) > 0.7:
        hits.append(("shouting", "review"))
    if re.search(r"(.)\1{9,}", raw):
        hits.append(("spam", "review"))
    toks = n.split()
    if len(toks) >= 8 and len(set(toks)) / len(toks) < 0.35:
        hits.append(("spam", "review"))

    if not hits:
        return Verdict(safe=True, action="allow", reason="ok", categories=[])
    action = max((a for _, a in hits), key=_RANK.__getitem__)
    reason = {
        "block": "This content breaks our community rules.",
        "review": "This content needs a quick review.",
    }[action]
    return Verdict(safe=False, action=action, reason=reason, categories=sorted({c for c, _ in hits}))
