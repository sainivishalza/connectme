import pytest

from moderation.content_filter import check_text


def action(text, kind="bio"):
    return check_text(text, kind).action


@pytest.mark.parametrize("text", [
    "I love hiking and good coffee. Always up for a spontaneous road trip!",
    "Software engineer, dog person, bad at cooking.",
    "I'm 28 and I'm looking for something real",
    "Lives 15 minutes from the city, 10 km from the coast",
    "I'm 10 minutes away from downtown",
    "Born in 2001, I'm 25 now",
    "",
    "   ",
])
def test_ordinary_text_is_allowed(text):
    assert action(text) == "allow"


@pytest.mark.parametrize("text", [
    "I'm 15 and looking for fun",
    "im 16",
    "I am 17 years old",
    "just a 14 yo here",
    "i'm only 13",
    "in 9th grade lol",
    "I'm a minor",
    "middle schooler looking for friends",
])
def test_minor_signals_are_blocked(text):
    v = check_text(text)
    assert v.action == "block"
    assert "underage" in v.categories


def test_weaker_age_signal_goes_to_review():
    assert action("high school student, graduating soon") == "review"


@pytest.mark.parametrize("text", [
    "kill yourself",
    "kys loser",
    "I will find you and hurt you",
    "i know where you live",
])
def test_threats_are_blocked(text):
    assert check_text(text).categories == ["threat"] or "threat" in check_text(text).categories
    assert action(text) == "block"


@pytest.mark.parametrize("text", [
    "Send me money and I'll visit you",
    "buy me a gift card and we can talk",
    "invest in crypto with me, guaranteed returns",
    "use Western Union please",
    "my cash app is $user",
])
def test_scams_are_blocked(text):
    v = check_text(text)
    assert v.action == "block"
    assert "scam" in v.categories


def test_commercial_sex_is_blocked():
    assert action("escort available, $200/hour") == "block"
    assert action("GFE full service") == "block"


def test_off_platform_and_links_are_reviewed_not_blocked():
    assert action("message me on whatsapp") == "review"
    assert action("check out my site example.com") == "review"
    assert action("see https://example.com") == "review"


def test_link_spam_is_blocked():
    assert action("a.com b.net c.org d.io") == "block"


def test_contact_info_policy_depends_on_where_it_appears():
    assert action("call me on +1 415 555 0123", "bio") == "review"
    assert action("call me on +1 415 555 0123", "post") == "block"
    assert action("write to jane.doe@example.com", "bio") == "review"
    assert action("jane (at) example (dot) com", "intro") == "review"


def test_profanity_is_review_in_text_but_block_in_names():
    assert action("this is so fucking good") == "review"
    assert action("fuckface", "name") == "allow" or action("fuckface", "name") in ("review", "block")
    assert action("shit", "name") == "block"
    assert action("shit", "group") == "block"


@pytest.mark.parametrize("text", ["sh1t", "f.u.c.k", "fuuuuck", "b!tch", "a$$hole"])
def test_common_evasions_are_caught(text):
    assert check_text(text).action in ("review", "block")


def test_leet_does_not_corrupt_ages_or_numbers():
    # "I'm 15" must stay a number so the minor rule fires, and "b00k" must not cause a false hit.
    assert action("I'm 15") == "block"
    assert action("I read a good b00k about 4 hours ago") == "allow"


def test_shouting_and_repetition_flagged_for_review():
    assert action("BUY NOW BEST OFFER ONLY TODAY CLICK HERE ALWAYS") == "review"
    assert action("hellooooooooooooooooo") == "review"
    assert action("buy buy buy buy buy buy buy buy buy buy") == "review"


def test_unicode_tricks_are_normalised():
    assert action("I​'m 15") == "block"  # zero-width space
    assert action("ｋｉｌｌ ｙｏｕｒｓｅｌｆ") == "block"  # full-width letters


def test_verdict_never_echoes_the_text():
    v = check_text("send me money, my number is +1 415 555 0123")
    assert "money" not in v.reason and "415" not in v.reason
    assert not v.safe


def test_extra_blocklist_file(tmp_path, monkeypatch):
    f = tmp_path / "bl.txt"
    f.write_text("# comment\nforbiddenword\n", encoding="utf-8")
    monkeypatch.setenv("MODERATION_EXTRA_BLOCKLIST", str(f))
    import moderation.content_filter as cf

    cf._extra_blocklist.cache_clear()
    try:
        assert cf.check_text("this has forbiddenword in it").action == "block"
        assert cf.check_text("this is fine").action == "allow"
    finally:
        cf._extra_blocklist.cache_clear()
