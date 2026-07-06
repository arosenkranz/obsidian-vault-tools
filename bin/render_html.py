#!/usr/bin/env python3
"""
render_html.py — Regenerate HTML guides from their paired Markdown source files.

Pairing convention: an HTML file contains a comment near the top:
    <!-- RENDER_SOURCE: relative/path/to/source.md -->
(path is relative to the vault root)

Usage:
    python3 render_html.py --vault /path/to/vault          # interactive picker
    python3 render_html.py --vault /path/to/vault --all    # regenerate all paired files
    python3 render_html.py --vault /path/to/vault --file "02-Areas/Learning/PO-20 Arcade Learning Guide.html"
"""

import argparse
import os
import re
import sys
from pathlib import Path
from datetime import date

# ─────────────────────────────────────────────────────────────────────────────
# ANSI colours (matching vault.sh palette)
# ─────────────────────────────────────────────────────────────────────────────
R  = "\033[0;31m"
G  = "\033[0;32m"
Y  = "\033[1;33m"
B  = "\033[0;34m"
C  = "\033[0;36m"
NC = "\033[0m"


# ─────────────────────────────────────────────────────────────────────────────
# Markdown → HTML helpers
# ─────────────────────────────────────────────────────────────────────────────

def escape_html(text: str) -> str:
    return (text
            .replace("&", "&amp;")
            .replace("<", "&lt;")
            .replace(">", "&gt;"))


def inline_md(text: str) -> str:
    """Convert inline Markdown (bold, italic, code, links, kbd shortcuts) to HTML."""
    # Wikilinks [[Target]] → plain text
    text = re.sub(r'\[\[([^\]|]+)(?:\|[^\]]+)?\]\]', r'\1', text)
    # Markdown links [label](url)
    text = re.sub(r'\[([^\]]+)\]\(([^)]+)\)',
                  r'<a href="\2" target="_blank">\1</a>', text)
    # **bold**
    text = re.sub(r'\*\*(.+?)\*\*', r'<strong>\1</strong>', text)
    # *italic*
    text = re.sub(r'\*(.+?)\*', r'<em>\1</em>', text)
    # `code` → <kbd> for button combos, <code> otherwise
    def code_sub(m):
        inner = m.group(1)
        # Treat as keyboard shortcut if it looks like button names (no spaces beyond +)
        if re.match(r'^[\w/\-]+([\s]*\+[\s]*[\w/\-]+)*$', inner):
            parts = re.split(r'\s*\+\s*', inner)
            return ' + '.join(f'<kbd>{p}</kbd>' for p in parts)
        return f'<code>{inner}</code>'
    text = re.sub(r'`([^`]+)`', code_sub, text)
    return text


def md_table_to_html(lines: list[str], css_class: str = "") -> str:
    """Convert a Markdown table (list of raw lines) to an HTML table."""
    rows = []
    for line in lines:
        line = line.strip().strip('|')
        if re.match(r'^[\s\-:|]+$', line):
            continue  # separator row
        cells = [c.strip() for c in line.split('|')]
        rows.append(cells)
    if not rows:
        return ''

    cls = f' class="{css_class}"' if css_class else ''
    html = [f'<div class="table-wrap"><table{cls}>']
    # First row = header
    html.append('<thead><tr>')
    for cell in rows[0]:
        html.append(f'<th>{inline_md(cell)}</th>')
    html.append('</tr></thead><tbody>')
    for row in rows[1:]:
        html.append('<tr>')
        for cell in row:
            html.append(f'<td>{inline_md(cell)}</td>')
        html.append('</tr>')
    html.append('</tbody></table></div>')
    return '\n'.join(html)


def md_to_html_body(md: str) -> str:
    """
    Convert the body of a Markdown document (after frontmatter) to an HTML
    fragment suitable for injection into the existing guide template.

    Strategy: walk line-by-line, collecting blocks, then emit HTML.
    """
    lines = md.splitlines()
    # Strip YAML frontmatter
    if lines and lines[0].strip() == '---':
        end = next((i for i, l in enumerate(lines[1:], 1) if l.strip() == '---'), None)
        if end is not None:
            lines = lines[end + 1:]

    html_parts = []
    i = 0

    def flush_paragraph(buf):
        if buf:
            text = ' '.join(buf).strip()
            if text:
                html_parts.append(f'<p>{inline_md(text)}</p>')

    para_buf = []
    section_counter = 0

    while i < len(lines):
        line = lines[i]
        stripped = line.strip()

        # ── H1 (document title — skip, already in header) ──
        if re.match(r'^# ', stripped):
            flush_paragraph(para_buf); para_buf = []
            i += 1
            continue

        # ── H2 (section headers) ──
        if re.match(r'^## ', stripped):
            flush_paragraph(para_buf); para_buf = []
            section_counter += 1
            title = re.sub(r'^## ', '', stripped)
            # Build an anchor id from the title
            anchor = re.sub(r'[^a-z0-9]+', '-', title.lower()).strip('-')
            num_str = f'{section_counter:02d}'
            html_parts.append(
                f'<section id="{anchor}">'
                f'<div class="section-header">'
                f'<div class="section-num">{num_str}</div>'
                f'<h2>{escape_html(title)}</h2>'
                f'</div>'
            )
            i += 1
            continue

        # ── H3 ──
        if re.match(r'^### ', stripped):
            flush_paragraph(para_buf); para_buf = []
            title = re.sub(r'^### ', '', stripped)
            html_parts.append(f'<h3>{inline_md(title)}</h3>')
            i += 1
            continue

        # ── Horizontal rule → close previous section, open nothing ──
        if re.match(r'^---+$', stripped):
            flush_paragraph(para_buf); para_buf = []
            if section_counter > 0:
                html_parts.append('</section>')
            i += 1
            continue

        # ── Blockquote (callout) ──
        if stripped.startswith('>'):
            flush_paragraph(para_buf); para_buf = []
            quote_lines = []
            while i < len(lines) and lines[i].strip().startswith('>'):
                quote_lines.append(lines[i].strip().lstrip('>').strip())
                i += 1
            content = ' '.join(quote_lines)
            html_parts.append(f'<div class="callout">{inline_md(content)}</div>')
            continue

        # ── Fenced code block ──
        if stripped.startswith('```'):
            flush_paragraph(para_buf); para_buf = []
            lang = stripped[3:].strip()
            code_lines = []
            i += 1
            while i < len(lines) and not lines[i].strip().startswith('```'):
                code_lines.append(lines[i])
                i += 1
            i += 1  # skip closing ```
            code = escape_html('\n'.join(code_lines))
            html_parts.append(
                f'<div class="device-wrap"><div class="device">'
                f'<pre>{code}</pre></div></div>'
            )
            continue

        # ── Markdown table ──
        if stripped.startswith('|') and i + 1 < len(lines) and re.match(r'^\|[\s\-:|]+\|', lines[i+1].strip()):
            flush_paragraph(para_buf); para_buf = []
            table_lines = []
            while i < len(lines) and lines[i].strip().startswith('|'):
                table_lines.append(lines[i])
                i += 1
            html_parts.append(md_table_to_html(table_lines))
            continue

        # ── Ordered list ──
        if re.match(r'^\d+\. ', stripped):
            flush_paragraph(para_buf); para_buf = []
            html_parts.append('<ol>')
            while i < len(lines) and re.match(r'^\d+\. ', lines[i].strip()):
                item = re.sub(r'^\d+\. ', '', lines[i].strip())
                html_parts.append(f'<li>{inline_md(item)}</li>')
                i += 1
            html_parts.append('</ol>')
            continue

        # ── Unordered list ──
        if re.match(r'^[-*] ', stripped):
            flush_paragraph(para_buf); para_buf = []
            html_parts.append('<ul>')
            while i < len(lines) and re.match(r'^[-*] ', lines[i].strip()):
                item = re.sub(r'^[-*] ', '', lines[i].strip())
                # ✅ win condition line
                if item.startswith('✅'):
                    html_parts.append(f'<div class="win">{inline_md(item)}</div>')
                else:
                    html_parts.append(f'<li>{inline_md(item)}</li>')
                i += 1
            html_parts.append('</ul>')
            continue

        # ── ✅ standalone win line ──
        if stripped.startswith('✅'):
            flush_paragraph(para_buf); para_buf = []
            html_parts.append(f'<div class="win">{inline_md(stripped)}</div>')
            i += 1
            continue

        # ── Empty line → flush paragraph ──
        if not stripped:
            flush_paragraph(para_buf); para_buf = []
            i += 1
            continue

        # ── Regular text line ──
        para_buf.append(stripped)
        i += 1

    flush_paragraph(para_buf)
    # Close any unclosed section
    if section_counter > 0:
        html_parts.append('</section>')

    return '\n'.join(html_parts)


# ─────────────────────────────────────────────────────────────────────────────
# HTML file manipulation
# ─────────────────────────────────────────────────────────────────────────────

RENDER_SOURCE_RE = re.compile(r'<!--\s*RENDER_SOURCE:\s*(.+?)\s*-->')
RENDER_BODY_RE   = re.compile(
    r'(<!--\s*RENDER_BODY_START\s*-->).*?(<!--\s*RENDER_BODY_END\s*-->)',
    re.DOTALL
)
RENDER_TS_RE     = re.compile(r'<!--\s*RENDER_TIMESTAMP:\s*.+?\s*-->')


def find_paired_files(vault_dir: Path) -> list[tuple[Path, Path]]:
    """Return [(html_path, md_path), ...] for all paired files in the vault."""
    pairs = []
    for html_file in vault_dir.rglob('*.html'):
        try:
            content = html_file.read_text(encoding='utf-8')
        except Exception:
            continue
        m = RENDER_SOURCE_RE.search(content)
        if m:
            rel_md = m.group(1).strip()
            md_path = vault_dir / rel_md
            pairs.append((html_file, md_path))
    return pairs


def regenerate(html_path: Path, md_path: Path) -> bool:
    """Regenerate html_path from md_path. Returns True on success."""
    if not md_path.exists():
        print(f'{R}✗ Markdown source not found: {md_path}{NC}')
        return False

    md_text   = md_path.read_text(encoding='utf-8')
    html_text = html_path.read_text(encoding='utf-8')

    # Check the required injection markers exist
    if not RENDER_BODY_RE.search(html_text):
        print(f'{Y}⚠  No RENDER_BODY markers found in {html_path.name} — skipping.{NC}')
        print(f'   Add <!-- RENDER_BODY_START --> and <!-- RENDER_BODY_END --> to the HTML.')
        return False

    new_body  = md_to_html_body(md_text)
    timestamp = f'<!-- RENDER_TIMESTAMP: {date.today().isoformat()} -->'

    # Inject body
    new_html = RENDER_BODY_RE.sub(
        r'\1\n' + new_body + r'\n\2',
        html_text
    )

    # Update or insert timestamp just after RENDER_SOURCE comment
    if RENDER_TS_RE.search(new_html):
        new_html = RENDER_TS_RE.sub(timestamp, new_html)
    else:
        new_html = RENDER_SOURCE_RE.sub(
            lambda m: m.group(0) + '\n' + timestamp,
            new_html,
            count=1
        )

    html_path.write_text(new_html, encoding='utf-8')
    rel_html = html_path.relative_to(html_path.parent.parent.parent) if html_path.parts[-3:] else html_path.name
    print(f'{G}✓ Rendered:{NC} {html_path.name}  ←  {md_path.name}')
    return True


# ─────────────────────────────────────────────────────────────────────────────
# CLI
# ─────────────────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(
        description='Regenerate HTML guides from paired Markdown sources.'
    )
    parser.add_argument('--vault', required=True, help='Path to the Obsidian vault root')
    parser.add_argument('--all',  action='store_true', help='Regenerate all paired HTML files')
    parser.add_argument('--file', help='Regenerate a specific HTML file (path relative to vault)')
    args = parser.parse_args()

    vault = Path(args.vault).expanduser().resolve()
    if not vault.is_dir():
        print(f'{R}✗ Vault directory not found: {vault}{NC}')
        sys.exit(1)

    pairs = find_paired_files(vault)

    # ── --file mode ──
    if args.file:
        target = vault / args.file
        match = [(h, m) for h, m in pairs if h == target]
        if not match:
            print(f'{R}✗ No RENDER_SOURCE comment found in {args.file}{NC}')
            print(f'  Make sure the HTML contains: <!-- RENDER_SOURCE: path/to/source.md -->')
            sys.exit(1)
        html_path, md_path = match[0]
        sys.exit(0 if regenerate(html_path, md_path) else 1)

    # ── --all mode ──
    if args.all:
        if not pairs:
            print(f'{Y}No paired HTML files found in vault.{NC}')
            print('  HTML files need a <!-- RENDER_SOURCE: path.md --> comment to be tracked.')
            sys.exit(0)
        ok = all(regenerate(h, m) for h, m in pairs)
        print(f'\n{G}Done.{NC} {len(pairs)} file(s) processed.')
        sys.exit(0 if ok else 1)

    # ── interactive picker ──
    if not pairs:
        print(f'{Y}No paired HTML files found in vault.{NC}')
        print('  HTML files need a <!-- RENDER_SOURCE: path.md --> comment to be tracked.')
        sys.exit(0)

    print(f'{C}Paired HTML guides:{NC}\n')
    for idx, (html_path, md_path) in enumerate(pairs, 1):
        exists = G if md_path.exists() else R
        print(f'  [{idx}] {html_path.name}')
        print(f'       {exists}← {md_path.relative_to(vault)}{NC}')
        print()

    print(f'  [a] All  [q] Quit\n')
    choice = input('Regenerate which? ').strip().lower()

    if choice == 'q':
        sys.exit(0)
    elif choice == 'a':
        ok = all(regenerate(h, m) for h, m in pairs)
        sys.exit(0 if ok else 1)
    elif choice.isdigit() and 1 <= int(choice) <= len(pairs):
        html_path, md_path = pairs[int(choice) - 1]
        sys.exit(0 if regenerate(html_path, md_path) else 1)
    else:
        print(f'{R}Invalid choice.{NC}')
        sys.exit(1)


if __name__ == '__main__':
    main()
