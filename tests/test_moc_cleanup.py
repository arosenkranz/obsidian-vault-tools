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


def test_validate_rejects_dropped_wikilink():
    missing_link = (
        "---\ntype: moc\n---\n"
        "# MOC Test\n\n"
        "## Resources\n\n"
        "- [[Foo Note]] — https://example.com/foo\n"
    )
    try:
        validate_proposal(ORIGINAL, missing_link)
        assert False, "expected ValidationError"
    except ValidationError as e:
        assert "Bar Note" in str(e)


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
