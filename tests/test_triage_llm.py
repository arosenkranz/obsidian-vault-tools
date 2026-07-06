"""Tests for bin/triage_llm.py — pure-logic pieces only (no live LLM calls,
no real vault). Run with: python3 -m pytest tests/test_triage_llm.py -v
"""

import sys
from pathlib import Path

# bin/ is not a package; import triage_llm by adding bin/ to sys.path.
sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "bin"))

from triage_llm import extract_moc_name_from_frontmatter, update_moc_entry_title


def test_smoke():
    assert 1 + 1 == 2


# ---------------------------------------------------------------------------
# extract_moc_name_from_frontmatter
# ---------------------------------------------------------------------------

def test_extract_moc_name_handles_wikilink_bracket_parse_quirk():
    # split_frontmatter's bracket-list heuristic mis-parses "moc: [[MOC Music]]"
    # as a one-item list whose single element is the string "[MOC Music]"
    # (see split_frontmatter's docstring/behavior) — this must still resolve
    # to a clean "MOC Music" name.
    fm = {"moc": ["[MOC Music]"]}
    assert extract_moc_name_from_frontmatter(fm) == "MOC Music"


def test_extract_moc_name_handles_plain_string_value():
    fm = {"moc": "[[MOC Music]]"}
    assert extract_moc_name_from_frontmatter(fm) == "MOC Music"


def test_extract_moc_name_returns_none_when_absent():
    assert extract_moc_name_from_frontmatter({}) is None
    assert extract_moc_name_from_frontmatter({"moc": ""}) is None


# ---------------------------------------------------------------------------
# update_moc_entry_title
# ---------------------------------------------------------------------------

def test_update_moc_entry_title_renames_matching_wikilink(tmp_path):
    moc_file = tmp_path / "MOC Test.md"
    moc_file.write_text(
        "---\ntype: moc\n---\n"
        "# MOC Test\n\n"
        "## 🔗 Resources\n\n"
        "- [[Old Title]] — some snippet\n"
        "- [[Unrelated Note]] — other snippet\n"
    )
    changed = update_moc_entry_title(moc_file, "Old Title", "New Title")
    assert changed is True
    text = moc_file.read_text()
    assert "[[New Title]]" in text
    assert "[[Old Title]]" not in text
    # Unrelated entries must survive untouched
    assert "[[Unrelated Note]] — other snippet" in text


def test_update_moc_entry_title_is_noop_when_title_unchanged(tmp_path):
    moc_file = tmp_path / "MOC Test.md"
    original = (
        "---\ntype: moc\n---\n"
        "# MOC Test\n\n"
        "## 🔗 Resources\n\n"
        "- [[Same Title]] — some snippet\n"
    )
    moc_file.write_text(original)
    changed = update_moc_entry_title(moc_file, "Same Title", "Same Title")
    assert changed is False
    assert moc_file.read_text() == original


def test_update_moc_entry_title_returns_false_when_no_match(tmp_path):
    moc_file = tmp_path / "MOC Test.md"
    original = (
        "---\ntype: moc\n---\n"
        "# MOC Test\n\n"
        "## 🔗 Resources\n\n"
        "- [[Some Other Note]] — some snippet\n"
    )
    moc_file.write_text(original)
    changed = update_moc_entry_title(moc_file, "Nonexistent Title", "New Title")
    assert changed is False
    assert moc_file.read_text() == original


def test_update_moc_entry_title_does_not_touch_frontmatter_or_other_links(tmp_path):
    moc_file = tmp_path / "MOC Test.md"
    moc_file.write_text(
        "---\ntype: moc\nrelated: [[Old Title]]\n---\n"
        "# MOC Test\n\n"
        "## 🔗 Resources\n\n"
        "- [[Old Title]] — some snippet\n"
    )
    update_moc_entry_title(moc_file, "Old Title", "New Title")
    text = moc_file.read_text()
    # Frontmatter block must be untouched even though it also contains the
    # literal string "[[Old Title]]" — only body wikilinks are renamed.
    fm_block = text.split("---\n", 2)[1]
    assert "[[Old Title]]" in fm_block
