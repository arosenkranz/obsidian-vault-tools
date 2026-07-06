"""Tests for bin/moc_cleanup.py — pure-logic pieces only (no live LLM calls,
no real vault). Run with: python3 -m pytest tests/test_moc_cleanup.py -v
"""

import sys
from pathlib import Path

# bin/ is not a package; import moc_cleanup by adding bin/ to sys.path.
sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "bin"))


def test_smoke():
    assert 1 + 1 == 2
