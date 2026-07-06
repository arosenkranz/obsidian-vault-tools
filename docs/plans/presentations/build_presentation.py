#!/usr/bin/env python3
"""
Builds a standalone HTML slide presentation from a writing-plans-format
markdown plan, with per-slide feedback captured in localStorage and an
export button that produces JSON + Markdown feedback dumps.

Usage:
    python3 build_presentation.py <plan.md> <output.html>
"""
import html
import json
import re
import sys
from pathlib import Path


def _split_top_level_headings(md_text: str) -> list[str]:
    """Split on lines starting with '## ' that are NOT inside a fenced code
    block (``` ... ```). Naive line-based splitting would also split on
    '## ' text that appears inside example code/heredocs in the plan (e.g.
    an example MOC file's own '## Resources' heading), producing bogus
    extra slides."""
    lines = md_text.split("\n")
    chunks: list[list[str]] = [[]]
    in_fence = False
    for line in lines:
        if re.match(r"^```", line):
            in_fence = not in_fence
        if not in_fence and re.match(r"^## ", line) and chunks[-1]:
            chunks.append([])
        chunks[-1].append(line)
    return ["\n".join(chunk) for chunk in chunks]


def split_into_slides(md_text: str) -> list[dict]:
    """Split the plan on top-level (##) headings. The doc title + Goal/
    Architecture/Tech Stack header block becomes slide 0. Fence-aware: a
    '## ' line inside a fenced code block does not start a new slide."""
    parts = _split_top_level_headings(md_text)
    slides = []
    for i, part in enumerate(parts):
        part = part.strip()
        if not part:
            continue
        if i == 0:
            # Title + Goal/Architecture/Tech Stack
            title_match = re.match(r"^#\s+(.+)$", part, re.MULTILINE)
            title = title_match.group(1) if title_match else "Plan"
            body = re.sub(r"^#\s+.+$", "", part, count=1, flags=re.MULTILINE).strip()
            body = body.rstrip("- \n")  # trailing '---'
            slides.append({"title": title, "kind": "title", "body": body})
        else:
            heading_match = re.match(r"^##\s+(.+)$", part, re.MULTILINE)
            title = heading_match.group(1) if heading_match else f"Slide {i}"
            body = re.sub(r"^##\s+.+$", "", part, count=1, flags=re.MULTILINE).strip()
            body = re.sub(r"\n---\s*$", "", body).strip()
            kind = "task" if title.lower().startswith("task ") else "section"
            slides.append({"title": title, "kind": kind, "body": body})
    return slides


def md_inline(text: str) -> str:
    """Minimal inline markdown -> HTML: **bold**, `code`, escape everything else."""
    text = html.escape(text)
    text = re.sub(r"\*\*(.+?)\*\*", r"<strong>\1</strong>", text)
    text = re.sub(r"`([^`]+?)`", r"<code>\1</code>", text)
    return text


def render_body(body: str) -> str:
    """Render a slide body: fenced code blocks become <pre><code>, checklist
    items become styled list items, everything else becomes paragraphs."""
    out = []
    lines = body.split("\n")
    i = 0
    in_list = False

    def close_list():
        nonlocal in_list
        if in_list:
            out.append("</ul>")
            in_list = False

    while i < len(lines):
        line = lines[i]

        # Fenced code block
        fence_match = re.match(r"^```(\w*)\s*$", line)
        if fence_match:
            close_list()
            lang = fence_match.group(1) or "text"
            code_lines = []
            i += 1
            while i < len(lines) and not lines[i].startswith("```"):
                code_lines.append(lines[i])
                i += 1
            i += 1  # skip closing ```
            code = "\n".join(code_lines)
            out.append(
                f'<pre class="code lang-{html.escape(lang)}"><code>{html.escape(code)}</code></pre>'
            )
            continue

        # Checklist step heading: - [ ] **Step N: ...**
        step_match = re.match(r"^- \[ \] \*\*(.+?)\*\*\s*$", line)
        if step_match:
            close_list()
            out.append(f'<div class="step"><span class="step-badge">STEP</span> {md_inline(step_match.group(1))}</div>')
            i += 1
            continue

        # Bullet list item
        bullet_match = re.match(r"^(\s*)-\s+(.+)$", line)
        if bullet_match:
            if not in_list:
                out.append("<ul>")
                in_list = True
            out.append(f"<li>{md_inline(bullet_match.group(2))}</li>")
            i += 1
            continue

        close_list()

        if line.strip() == "":
            i += 1
            continue

        # Sub-heading (### ...)
        sub_match = re.match(r"^###\s+(.+)$", line)
        if sub_match:
            out.append(f"<h4>{md_inline(sub_match.group(1))}</h4>")
            i += 1
            continue

        # "**Files:**" style bold-label lines and plain paragraphs
        out.append(f"<p>{md_inline(line)}</p>")
        i += 1

    close_list()
    return "\n".join(out)


def build_html(slides: list[dict], plan_title: str, plan_source: str) -> str:
    slide_html_blocks = []
    nav_items = []
    for idx, slide in enumerate(slides):
        body_html = render_body(slide["body"])
        kind = slide["kind"]
        slide_html_blocks.append(f"""
        <section class="slide {kind}" data-index="{idx}" id="slide-{idx}">
          <div class="slide-inner">
            <div class="slide-header">
              <span class="slide-kind">{kind}</span>
              <span class="slide-counter">{idx + 1} / {len(slides)}</span>
            </div>
            <h2>{md_inline(slide['title'])}</h2>
            <div class="slide-body">{body_html}</div>
          </div>
          <div class="feedback-panel">
            <label for="fb-{idx}">Feedback on this slide</label>
            <textarea id="fb-{idx}" data-index="{idx}" placeholder="Type feedback for this slide… saved automatically."></textarea>
            <div class="feedback-status" id="fb-status-{idx}"></div>
          </div>
        </section>""")
        short_title = slide["title"][:40] + ("…" if len(slide["title"]) > 40 else "")
        nav_items.append(f'<li><a href="#slide-{idx}" data-index="{idx}" class="nav-link">{idx + 1}. {html.escape(short_title)}</a></li>')

    slides_markup = "\n".join(slide_html_blocks)
    nav_markup = "\n".join(nav_items)
    slides_json = json.dumps([s["title"] for s in slides])

    return f"""<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>{html.escape(plan_title)} — Review</title>
<style>
  :root {{
    --bg: #0f1117;
    --panel: #171a23;
    --panel-2: #1e2230;
    --border: #2a2f3f;
    --text: #e6e8ef;
    --text-dim: #9aa0b4;
    --accent: #7aa2f7;
    --green: #4ade80;
    --red: #f87171;
    --yellow: #fbbf24;
    --code-bg: #0b0d12;
    --radius: 10px;
  }}
  * {{ box-sizing: border-box; }}
  html, body {{
    margin: 0; padding: 0;
    background: var(--bg);
    color: var(--text);
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif;
    scroll-behavior: smooth;
  }}
  a {{ color: var(--accent); text-decoration: none; }}
  a:hover {{ text-decoration: underline; }}

  #topbar {{
    position: sticky; top: 0; z-index: 20;
    display: flex; align-items: center; justify-content: space-between;
    padding: 10px 20px;
    background: rgba(15,17,23,0.92);
    backdrop-filter: blur(6px);
    border-bottom: 1px solid var(--border);
  }}
  #topbar .title {{ font-weight: 600; font-size: 14px; color: var(--text-dim); }}
  #topbar .actions {{ display: flex; gap: 8px; }}
  button {{
    background: var(--panel-2); color: var(--text); border: 1px solid var(--border);
    border-radius: 8px; padding: 8px 14px; font-size: 13px; cursor: pointer;
    font-family: inherit;
  }}
  button:hover {{ border-color: var(--accent); }}
  button.primary {{ background: var(--accent); color: #0f1117; border-color: var(--accent); font-weight: 600; }}
  button.primary:hover {{ opacity: 0.9; }}

  #layout {{ display: flex; }}
  #sidebar {{
    width: 260px; flex-shrink: 0;
    position: sticky; top: 49px;
    height: calc(100vh - 49px);
    overflow-y: auto;
    border-right: 1px solid var(--border);
    padding: 16px 8px;
  }}
  #sidebar ul {{ list-style: none; margin: 0; padding: 0; }}
  #sidebar li a {{
    display: block; padding: 7px 12px; border-radius: 8px; font-size: 13px;
    color: var(--text-dim); white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }}
  #sidebar li a:hover {{ background: var(--panel-2); color: var(--text); text-decoration: none; }}
  #sidebar li a.active {{ background: var(--panel-2); color: var(--accent); font-weight: 600; }}
  #sidebar li a.has-feedback::after {{ content: " ●"; color: var(--yellow); }}

  #main {{ flex: 1; min-width: 0; padding: 24px 24px 120px; max-width: 980px; }}

  .slide {{
    background: var(--panel);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    margin-bottom: 28px;
    padding: 28px 32px;
    scroll-margin-top: 60px;
  }}
  .slide.title {{ border-color: var(--accent); }}
  .slide-header {{
    display: flex; justify-content: space-between; align-items: center;
    margin-bottom: 6px;
  }}
  .slide-kind {{
    font-size: 11px; text-transform: uppercase; letter-spacing: 0.08em;
    color: var(--text-dim); font-weight: 600;
  }}
  .slide-counter {{ font-size: 12px; color: var(--text-dim); }}
  .slide h2 {{ margin: 4px 0 16px; font-size: 22px; }}
  .slide h4 {{ margin: 18px 0 8px; font-size: 15px; color: var(--accent); }}
  .slide p {{ line-height: 1.55; margin: 8px 0; color: var(--text); }}
  .slide ul {{ padding-left: 22px; margin: 8px 0; }}
  .slide li {{ margin: 4px 0; line-height: 1.5; }}
  .slide code {{
    background: var(--code-bg); padding: 2px 6px; border-radius: 5px;
    font-size: 0.85em; font-family: "SF Mono", Menlo, Consolas, monospace;
  }}
  pre.code {{
    background: var(--code-bg); border: 1px solid var(--border); border-radius: 8px;
    padding: 14px 16px; overflow-x: auto; margin: 10px 0;
  }}
  pre.code code {{
    background: none; padding: 0; font-size: 13px; line-height: 1.5;
    font-family: "SF Mono", Menlo, Consolas, monospace; white-space: pre;
  }}
  .step {{
    margin: 16px 0 4px; font-weight: 600; font-size: 14px;
    display: flex; align-items: center; gap: 8px;
  }}
  .step-badge {{
    background: var(--accent); color: #0f1117; font-size: 10px; font-weight: 700;
    padding: 2px 7px; border-radius: 5px; letter-spacing: 0.05em;
  }}

  .feedback-panel {{
    margin-top: 22px; padding-top: 16px; border-top: 1px dashed var(--border);
  }}
  .feedback-panel label {{
    display: block; font-size: 12px; color: var(--text-dim); margin-bottom: 6px;
    text-transform: uppercase; letter-spacing: 0.04em; font-weight: 600;
  }}
  .feedback-panel textarea {{
    width: 100%; min-height: 64px; resize: vertical;
    background: var(--panel-2); color: var(--text);
    border: 1px solid var(--border); border-radius: 8px;
    padding: 10px 12px; font-family: inherit; font-size: 13px; line-height: 1.4;
  }}
  .feedback-panel textarea:focus {{ outline: none; border-color: var(--accent); }}
  .feedback-status {{ font-size: 11px; color: var(--green); height: 16px; margin-top: 4px; }}

  #progress-pill {{
    font-size: 12px; color: var(--text-dim); padding: 6px 12px;
  }}

  @media (max-width: 800px) {{
    #sidebar {{ display: none; }}
    #main {{ padding: 16px; max-width: none; }}
  }}
</style>
</head>
<body>

<div id="topbar">
  <div class="title">📋 {html.escape(plan_title)} <span id="progress-pill"></span></div>
  <div class="actions">
    <button id="btn-export-md">Export feedback (Markdown)</button>
    <button id="btn-export-json">Export feedback (JSON)</button>
    <button id="btn-clear" title="Clear all saved feedback">Clear feedback</button>
  </div>
</div>

<div id="layout">
  <nav id="sidebar">
    <ul>
{nav_markup}
    </ul>
  </nav>
  <main id="main">
{slides_markup}
  </main>
</div>

<script>
const SLIDE_TITLES = {slides_json};
const STORAGE_KEY = "moc-cleanup-plan-feedback";
const PLAN_SOURCE = {json.dumps(plan_source)};

function loadFeedback() {{
  try {{
    return JSON.parse(localStorage.getItem(STORAGE_KEY)) || {{}};
  }} catch (e) {{ return {{}}; }}
}}
function saveFeedback(data) {{
  localStorage.setItem(STORAGE_KEY, JSON.stringify(data));
}}

function updateNavDots(data) {{
  document.querySelectorAll("#sidebar a.nav-link").forEach(a => {{
    const idx = a.dataset.index;
    const has = data[idx] && data[idx].trim().length > 0;
    a.classList.toggle("has-feedback", !!has);
  }});
  const count = Object.values(data).filter(v => v && v.trim().length > 0).length;
  document.getElementById("progress-pill").textContent =
    count > 0 ? `· ${{count}} comment${{count === 1 ? "" : "s"}}` : "";
}}

document.addEventListener("DOMContentLoaded", () => {{
  const feedback = loadFeedback();

  // Hydrate textareas from storage
  document.querySelectorAll(".feedback-panel textarea").forEach(ta => {{
    const idx = ta.dataset.index;
    if (feedback[idx]) ta.value = feedback[idx];

    let saveTimer = null;
    ta.addEventListener("input", () => {{
      const status = document.getElementById(`fb-status-${{idx}}`);
      status.textContent = "typing…";
      clearTimeout(saveTimer);
      saveTimer = setTimeout(() => {{
        const data = loadFeedback();
        data[idx] = ta.value;
        saveFeedback(data);
        status.textContent = "✓ saved";
        updateNavDots(data);
        setTimeout(() => {{ if (status.textContent === "✓ saved") status.textContent = ""; }}, 1500);
      }}, 400);
    }});
  }});

  updateNavDots(feedback);

  // Active-slide highlighting in sidebar via IntersectionObserver
  const links = document.querySelectorAll("#sidebar a.nav-link");
  const observer = new IntersectionObserver((entries) => {{
    entries.forEach(entry => {{
      if (entry.isIntersecting) {{
        const idx = entry.target.dataset.index;
        links.forEach(l => l.classList.toggle("active", l.dataset.index === idx));
      }}
    }});
  }}, {{ rootMargin: "-40% 0px -50% 0px" }});
  document.querySelectorAll(".slide").forEach(s => observer.observe(s));

  document.getElementById("btn-clear").addEventListener("click", () => {{
    if (confirm("Clear all saved feedback for this plan review?")) {{
      localStorage.removeItem(STORAGE_KEY);
      document.querySelectorAll(".feedback-panel textarea").forEach(ta => ta.value = "");
      updateNavDots({{}});
    }}
  }});

  function collect() {{
    const data = loadFeedback();
    const items = [];
    SLIDE_TITLES.forEach((title, idx) => {{
      const text = (data[idx] || "").trim();
      if (text) items.push({{ index: idx, title, feedback: text }});
    }});
    return items;
  }}

  function download(filename, content, mime) {{
    const blob = new Blob([content], {{ type: mime }});
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url; a.download = filename;
    document.body.appendChild(a); a.click(); document.body.removeChild(a);
    URL.revokeObjectURL(url);
  }}

  document.getElementById("btn-export-json").addEventListener("click", () => {{
    const items = collect();
    if (items.length === 0) {{ alert("No feedback to export yet."); return; }}
    download("moc-cleanup-plan-feedback.json", JSON.stringify(items, null, 2), "application/json");
  }});

  document.getElementById("btn-export-md").addEventListener("click", () => {{
    const items = collect();
    if (items.length === 0) {{ alert("No feedback to export yet."); return; }}
    let md = "# Feedback on: {html.escape(plan_title)}\\n\\n";
    items.forEach(item => {{
      md += `## Slide ${{item.index + 1}}: ${{item.title}}\\n\\n${{item.feedback}}\\n\\n`;
    }});
    download("moc-cleanup-plan-feedback.md", md, "text/markdown");
  }});
}});
</script>

</body>
</html>
"""


def main():
    if len(sys.argv) != 3:
        print("Usage: build_presentation.py <plan.md> <output.html>", file=sys.stderr)
        return 2
    plan_path = Path(sys.argv[1])
    out_path = Path(sys.argv[2])
    md_text = plan_path.read_text()

    title_match = re.match(r"^#\s+(.+)$", md_text, re.MULTILINE)
    plan_title = title_match.group(1) if title_match else plan_path.stem

    slides = split_into_slides(md_text)
    out_html = build_html(slides, plan_title, str(plan_path))
    out_path.write_text(out_html)
    print(f"Wrote {len(slides)} slides to {out_path}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
