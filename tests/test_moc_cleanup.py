"""Tests for bin/moc_cleanup.py — pure-logic pieces only (no live LLM calls,
no real vault). Run with: python3 -m pytest tests/test_moc_cleanup.py -v
"""

import sys
from pathlib import Path

# bin/ is not a package; import moc_cleanup by adding bin/ to sys.path.
sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "bin"))


def test_smoke():
    assert 1 + 1 == 2


from moc_cleanup import build_prompt


def test_build_prompt_includes_full_moc_content():
    moc_text = "---\ntype: moc\n---\n# MOC Test\n\n## Resources\n\n- [[foo]] — bar\n"
    prompt = build_prompt(moc_text, moc_name="MOC Test")
    assert moc_text in prompt


def test_build_prompt_states_forbidden_operations():
    prompt = build_prompt("content", moc_name="MOC Test")
    assert "must not delete" in prompt.lower() or "never delete" in prompt.lower()
    assert "frontmatter" in prompt.lower()


def test_build_prompt_requests_json_shape():
    prompt = build_prompt("content", moc_name="MOC Test")
    assert '"new_content"' in prompt
    assert '"duplicates_flagged"' in prompt


from moc_cleanup import validate_proposal, ValidationError

ORIGINAL = (
    "---\ntype: moc\n---\n"
    "# MOC Test\n\n"
    "## Resources\n\n"
    "- [[Foo Note]] — https://example.com/foo\n"
    "- [[Bar Note]] — https://example.com/bar\n"
)


def test_validate_accepts_reordering_that_keeps_all_links():
    reordered = (
        "---\ntype: moc\n---\n"
        "# MOC Test\n\n"
        "## Resources\n\n"
        "### Example.com\n"
        "- [[Bar Note]] — https://example.com/bar\n"
        "- [[Foo Note]] — https://example.com/foo\n"
    )
    # Should not raise
    validate_proposal(ORIGINAL, reordered)


def test_validate_rejects_dropped_entry_url_and_wikilink_together():
    # Deleting an entire entry drops both its wikilink and its URL — the
    # URL check is what actually catches this (URL-anchored entries are no
    # longer checked by wikilink text, see test_validate_accepts_retitling_
    # a_url_anchored_wikilink), but the outcome must still be a rejection.
    missing_entry = (
        "---\ntype: moc\n---\n"
        "# MOC Test\n\n"
        "## Resources\n\n"
        "- [[Foo Note]] — https://example.com/foo\n"
    )
    try:
        validate_proposal(ORIGINAL, missing_entry)
        assert False, "expected ValidationError"
    except ValidationError as e:
        assert "https://example.com/bar" in str(e)


def test_validate_rejects_frontmatter_change():
    changed_fm = (
        "---\ntype: moc\nextra: field\n---\n"
        "# MOC Test\n\n"
        "## Resources\n\n"
        "- [[Foo Note]] — https://example.com/foo\n"
        "- [[Bar Note]] — https://example.com/bar\n"
    )
    try:
        validate_proposal(ORIGINAL, changed_fm)
        assert False, "expected ValidationError"
    except ValidationError as e:
        assert "frontmatter" in str(e).lower()


def test_validate_accepts_retitling_a_url_anchored_wikilink():
    # The whole point of the garbled-title-fix feature: a wikilink sharing
    # a line with a URL may be renamed freely, since the URL still anchors
    # the entry to the same content.
    retitled = (
        "---\ntype: moc\n---\n"
        "# MOC Test\n\n"
        "## Resources\n\n"
        "- [[Foo Article]] — https://example.com/foo\n"
        "- [[Bar Note]] — https://example.com/bar\n"
    )
    # Should not raise
    validate_proposal(ORIGINAL, retitled)


def test_validate_rejects_renamed_bare_wikilink_with_no_url():
    # A bare wikilink (no URL on its line) is the only anchor for that
    # entry — usually a link to another vault note. Renaming it would
    # silently break the real Obsidian link, so it must survive verbatim.
    original_with_bare_link = (
        "---\ntype: moc\n---\n"
        "# MOC Test\n\n"
        "## Resources\n\n"
        "- [[Neovim]] — my editor setup\n"
    )
    renamed_bare_link = (
        "---\ntype: moc\n---\n"
        "# MOC Test\n\n"
        "## Resources\n\n"
        "- [[Neovim Notes]] — my editor setup\n"
    )
    try:
        validate_proposal(original_with_bare_link, renamed_bare_link)
        assert False, "expected ValidationError"
    except ValidationError as e:
        assert "Neovim" in str(e)


def test_validate_rejects_dropped_url():
    # Title changed AND its URL vanished entirely — url count check catches
    # cases the wikilink check might miss (e.g. plain URLs in prose).
    missing_url = (
        "---\ntype: moc\n---\n"
        "# MOC Test\n\n"
        "## Resources\n\n"
        "- [[Foo Note]] — https://example.com/foo\n"
        "- [[Bar Note]] — some text with no url\n"
    )
    try:
        validate_proposal(ORIGINAL, missing_url)
        assert False, "expected ValidationError"
    except ValidationError as e:
        assert "url" in str(e).lower() or "link" in str(e).lower()


from moc_cleanup import render_diff


def test_render_diff_shows_added_and_removed_lines():
    old = "line1\nline2\nline3\n"
    new = "line1\nline2-changed\nline3\n"
    out = render_diff(old, new, filename="MOC Test.md")
    assert "-line2\n" in out or "-line2" in out
    assert "+line2-changed" in out


def test_render_diff_empty_when_identical():
    same = "line1\nline2\n"
    out = render_diff(same, same, filename="MOC Test.md")
    assert out.strip() == "" or "no changes" in out.lower()


from moc_cleanup import parse_llm_response


def test_parse_llm_response_extracts_new_content():
    raw = '{"new_content": "hello", "duplicates_flagged": [], "summary": "ok"}'
    result = parse_llm_response(raw)
    assert result["new_content"] == "hello"
    assert result["duplicates_flagged"] == []


def test_parse_llm_response_handles_fenced_json():
    raw = '```json\n{"new_content": "hi", "duplicates_flagged": [], "summary": "x"}\n```'
    result = parse_llm_response(raw)
    assert result["new_content"] == "hi"


def test_parse_llm_response_rejects_missing_new_content():
    raw = '{"duplicates_flagged": [], "summary": "x"}'
    try:
        parse_llm_response(raw)
        assert False, "expected ValueError"
    except ValueError as e:
        assert "new_content" in str(e)
