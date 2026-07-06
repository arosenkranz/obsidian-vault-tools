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
