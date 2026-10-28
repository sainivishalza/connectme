import pytest

from scam.detector import check
from schemas import ScamCheckRequest, ScamLine


def run(*lines, me=()):
    ls = [ScamLine(who="other", text=t) for t in lines] + [ScamLine(who="me", text=t) for t in me]
    return check(ScamCheckRequest(lines=ls))


def test_the_classic_script_is_high_risk_and_explained():
    r = run("I love you, you are my everything", "I am working on an oil rig overseas and my camera is broken",
            "I need some money urgently for customs fee, please don't tell anyone", "Add me on WhatsApp")
    assert r.risk == "high" and r.score >= 60
    codes = [s.code for s in r.signals]
    assert "money" in codes and len(r.signals) <= 4
    assert all(s.text for s in r.signals) and "money" in r.advice.lower()


@pytest.mark.parametrize("text,code", [
    ("necesito dinero urgente, envíame plata", "money"),
    ("mujhe paise chahiye, udhaar de do", "money"),
    ("मुझे पैसे भेज दो", "money"),
    ("send me your OTP and Aadhaar", "documents"),
    ("guaranteed returns on this bitcoin trading platform", "crypto"),
    ("hablemos por whatsapp", "off_platform"),
])
def test_signals_in_three_languages(text, code):
    assert code in [s.code for s in run(text).signals]


def test_ordinary_conversation_is_low_risk():
    r = run("Hey! How was your weekend?", "I went hiking with my sister, it was lovely", "Do you like jazz? There's a gig on Friday")
    assert r.risk == "low" and r.score == 0 and r.signals == []


def test_one_weak_signal_alone_is_not_alarming():
    assert run("I'm so tired, work was urgent today").risk == "low"
    assert run("let's continue on WhatsApp").risk == "low"


def test_only_what_the_other_person_wrote_counts():
    r = run("How are you?", me=("I will never send you money or gift cards", "I love you"))
    assert r.risk == "low" and r.signals == []


def test_money_plus_pressure_rises_above_either_alone():
    alone = run("can you send me some money").score
    together = run("can you send me some money", "it is an emergency, don't tell anyone").score
    assert together > alone + 20


def test_request_limits():
    with pytest.raises(Exception):
        ScamCheckRequest(lines=[])
    with pytest.raises(Exception):
        ScamCheckRequest(lines=[ScamLine(who="other", text="x")] * 11)
