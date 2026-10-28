"""Scam check for a few chat lines a member chose to share.

Romance scams follow a script more than they use any one word, so this looks
at the *combination* of signals in what the other person wrote: love declared
very early, a story that explains why they cannot meet or call, urgency, a
request to leave the app, and then money or documents. One signal alone is
weak; several together are strong. It reads English, Spanish and Hindi
(Devanagari and typed Latin spelling).

It is a guide, not a verdict: it will miss scams and flag honest people, so it
explains *why* it is worried and always says what to do, and it never blocks
anything. Lines are not stored or logged.
"""
from __future__ import annotations

import re
from dataclasses import dataclass

from schemas import ScamCheckRequest, ScamCheckResponse, ScamSignal


@dataclass(frozen=True)
class _Rule:
    code: str
    weight: int
    text: str
    pattern: re.Pattern[str]


def _r(code: str, weight: int, text: str, pattern: str) -> _Rule:
    return _Rule(code, weight, text, re.compile(pattern, re.IGNORECASE))


RULES: list[_Rule] = [
    _r("money", 40, "They asked for money, a gift card or a payment",
       r"\b(gift ?cards?|wire (me|the|some)|western union|money ?gram|send (me )?(some )?(money|cash)|lend me|loan me|need (some )?(money|cash)"
       r"|pay (for )?my|bank (details|account|transfer)|transfer (the )?(money|funds)|processing fee|customs fee"
       r"|env[ií]ame (dinero|plata)|necesito (dinero|plata)|pr[eé]stame|transferencia|tarjeta de regalo"
       r"|paise bhej|paisa bhej|paise chahiye|udhaar|udhar de)\b|पैसे (भेज|चाहिए)|उधार|गिफ्ट कार्ड"),
    _r("crypto", 35, "They are pushing an investment or cryptocurrency",
       r"\b(bitcoin|btc|crypto(currency)?|usdt|forex|trading (platform|app|account)|invest(ment)? (opportunity|platform|with me|app)|guaranteed returns?"
       r"|inversi[oó]n segura|criptomonedas?|nivesh)\b|क्रिप्टो|निवेश"),
    _r("documents", 35, "They asked for ID, a one-time code or card details",
       r"\b(otp|one[- ]time (code|password)|verification code|send (me )?(your )?(id|passport|licen[cs]e|ssn|card number|pin|password)"
       r"|aadhaar|aadhar|pan card|cvv|n[uú]mero de (tarjeta|cuenta)|env[ií]ame (tu )?(dni|pasaporte|contrase[nñ]a))\b|ओटीपी|आधार|पासवर्ड भेज"),
    _r("urgency", 15, "They are rushing you",
       r"\b(urgent(ly)?|right now|immediately|emergency|as soon as possible|asap|before (it'?s )?too late|no time|today only"
       r"|urgente|ahora mismo|emergencia|jaldi|abhi turant|turant)\b|जल्दी|तुरंत|आपातकाल"),
    _r("secrecy", 20, "They asked you to keep this secret",
       r"\b(don'?t tell (anyone|your (family|friends))|keep (this|it) (a )?secret|between (us|you and me)|our little secret"
       r"|no le (digas|cuentes) a nadie|es (un )?secreto|kisi ko mat batana|kisi ko na batao)\b|किसी को मत बताना|राज़ रखना"),
    _r("off_platform", 15, "They want to move to another app or a link",
       r"\b(whats ?app|telegram|signal|text me on|add me on|hangouts|wechat|line id|snap(chat)?|kik|move (this )?(chat|conversation) (to|off)"
       r"|h[aá]blame por|escr[ií]beme (a|por)|mera number|whatsapp par)\b|https?://\S+|व्हाट्सएप|टेलीग्राम"),
    _r("love_bombing", 15, "They declared strong feelings very quickly",
       r"\b(i love you|i'?m in love|soul ?mate|love of my life|meant to be|you are my everything|my queen|my angel|my darling"
       r"|te amo|alma gemela|eres el amor de mi vida|mujhe tumse pyar|i luv u)\b|मुझे तुमसे प्यार|मेरी जान"),
    _r("cant_meet", 20, "They have reasons why they cannot meet or video call",
       r"\b((can'?t|cannot|unable to) (video ?call|meet|see you|talk on (the )?phone|call)|camera (is )?(broken|not working)"
       r"|no puedo (verte|hacer videollamada|llamar)|video call nahi|mil nahi sakta)\b|वीडियो कॉल नहीं"),
    _r("backstory", 20, "Their story matches a common scam script (overseas work, military, oil rig, inheritance, a stuck relative)",
       r"\b(oil rig|offshore|deployed|peacekeep|army (officer|general)|navy|working abroad|overseas (contract|job|work)|inheritance|"
       r"stuck (at|in) (the )?(airport|customs|hotel)|my (daughter|son|mother|father) is (sick|in hospital)|surgery|medical bills?|hospital bills?"
       r"|trabajo en el extranjero|herencia|plataforma petrolera|bahar kaam|videsh)\b|विदेश"),
]

_CAPS = {"money": 1, "crypto": 1, "documents": 1}  # one hit is enough; they are already heavy

ADVICE = {
    "high": "Please stop and do not send money, codes or documents. Talk to a friend, block and report this person if you are unsure.",
    "medium": "Be careful. Keep chatting here, do not send money or leave the app, and ask for a live video call before you trust them.",
    "low": "Nothing here matches common scam patterns, but that is not a guarantee. Never send money to someone you have not met.",
}


def check(req: ScamCheckRequest) -> ScamCheckResponse:
    other = [ln.text for ln in req.lines if ln.who == "other"]
    text = "\n".join(other)
    signals: list[ScamSignal] = []
    score = 0
    for rule in RULES:
        if rule.pattern.search(text):
            signals.append(ScamSignal(code=rule.code, text=rule.text))
            score += rule.weight
    # Signals that mean little alone mean a lot together: a story, a move off
    # the app and a request for money is the whole scam in three messages.
    codes = {s.code for s in signals}
    if "money" in codes and codes & {"urgency", "backstory", "secrecy", "love_bombing", "off_platform", "cant_meet"}:
        score += 15
    if "love_bombing" in codes and "off_platform" in codes:
        score += 10
    score = min(score, 100)
    risk = "high" if score >= 60 else "medium" if score >= 30 else "low"
    # Strongest reasons first, at most four.
    weights = {r.code: r.weight for r in RULES}
    signals.sort(key=lambda s: weights[s.code], reverse=True)
    return ScamCheckResponse(risk=risk, score=score, signals=signals[:4], advice=ADVICE[risk])
