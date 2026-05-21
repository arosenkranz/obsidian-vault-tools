#!/bin/bash

# vault - Obsidian PARA vault management CLI
# Repo:    ~/workspace/obsidian-vault-tools/
# Symlink: ~/.local/bin/ov  (created by `make install`)
# Config:  ~/.config/ov/config  (set OV_VAULT_DIR; see examples/ov.config.example)

set -e

# Resolve the script location, handling symlinks (so we can find triage_llm.py next to us)
SOURCE="${BASH_SOURCE[0]}"
while [ -h "$SOURCE" ]; do
  DIR="$(cd -P "$(dirname "$SOURCE")" && pwd)"
  SOURCE="$(readlink "$SOURCE")"
  [[ $SOURCE != /* ]] && SOURCE="$DIR/$SOURCE"
done
SCRIPT_DIR="$(cd -P "$(dirname "$SOURCE")" && pwd)"

# Load per-machine config: ~/.config/ov/config (or $OV_CONFIG if set).
# The file is sourced as bash, so it can set OV_* variables.
load_config() {
    local config_file="${OV_CONFIG:-$HOME/.config/ov/config}"
    if [ -f "$config_file" ]; then
        # shellcheck disable=SC1090
        source "$config_file"
    fi
    if [ -z "${OV_VAULT_DIR:-}" ]; then
        echo "ov: OV_VAULT_DIR not set." >&2
        echo "    Create $config_file (see ov.config.example) or export OV_VAULT_DIR." >&2
        exit 1
    fi
    # Expand ~ and env vars in vault path
    eval "VAULT_DIR=\"$OV_VAULT_DIR\""
    INBOX_DIR="$VAULT_DIR/${OV_INBOX:-00-Inbox}"
    PROJECTS_DIR="$VAULT_DIR/${OV_PROJECTS:-01-Projects}"
    AREAS_DIR="$VAULT_DIR/${OV_AREAS:-02-Areas}"
    RESOURCES_DIR="$VAULT_DIR/${OV_RESOURCES:-03-Resources}"
    ARCHIVE_DIR="$VAULT_DIR/${OV_ARCHIVE:-04-Archive}"
    META_DIR="$VAULT_DIR/${OV_META:-99-Meta}"
}
load_config

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

show_help() {
    cat << EOF
ov - Obsidian Vault management

USAGE:
    ov <command> [options]

COMMANDS:
    inbox               List inbox contents with age
    capture [text]      Quick-dump a note into the inbox (body via arg or stdin)
    triage [--llm]      Interactively process inbox notes (--llm uses Claude)
    new                 Create new note from template
    review              Weekly review summary
    stale [days]        Find notes not touched in N days (default: 90)
    mocs <subcommand>   Manage Maps of Content
    publish [<file>]    Publish to docs server; opens picker if no file given
    unpublish [<file>]  Remove file(s) from docs server; opens picker if no files given
    help                Show this help

PUBLISH OPTIONS:
    --llm               Convert a .md note to HTML via LLM before publishing
    --desc "<text>"     Design guidance for --llm (default: clean, modern design)

CAPTURE OPTIONS:
    --title <str>       Explicit title (otherwise auto-derived from first line)
    --tags <a,b,c>      Comma-separated tags (optional)
    --source <name>     Source label: cli | web | llm (default: cli)

MOC SUBCOMMANDS:
    mocs list           List all MOCs with descriptions
    mocs new            Create new MOC from template
    mocs orphan         Find notes not linked from any MOC
    mocs add            Add note to MOC interactively
    mocs update         Update MOCs in directory

EXAMPLES:
    ov inbox                              # Show inbox contents
    ov capture "quick thought"            # One-shot capture
    echo "..." | ov capture --title Foo   # Capture from stdin
    ov capture --source llm <<EOF         # From an LLM session
    body...
    EOF
    ov triage                             # Process inbox interactively
    ov new                                # Create new note
    ov stale 60                           # Find notes untouched for 60+ days
    ov mocs orphan                        # Find unlinked notes
EOF
}

# Get file age in days - simplified approach
get_file_age() {
    local file="$1"
    
    # Use find command which is more portable
    local days_old=$(find "$file" -mtime +0 -exec echo "1" \; 2>/dev/null | wc -l | tr -d ' ')
    
    # If find method doesn't work, try a simpler approach
    if [ -z "$days_old" ] || [ "$days_old" -eq 0 ]; then
        # Try using date -r (macOS) or stat (Linux)
        local mod_time
        if [[ "$OSTYPE" == "darwin"* ]]; then
            mod_time=$(date -r "$file" +%s 2>/dev/null)
        else
            mod_time=$(stat -c %Y "$file" 2>/dev/null)
        fi
        
        if [ -n "$mod_time" ]; then
            local now=$(date +%s)
            days_old=$(( (now - mod_time) / 86400 ))
        else
            days_old=0
        fi
    fi
    
    echo ${days_old:-0}
}

# Format file age for display
format_age() {
    local age=$1
    if [ $age -gt 7 ]; then
        echo -e "${RED}⚠${NC}  "
    else
        echo -e "•  "
    fi
}

# Slugify a title for filenames: keep letters/numbers/spaces, collapse whitespace, trim.
# Preserves Title Case the user gave us; we do not lowercase.
slugify_title() {
    local raw="$1"
    # Strip leading markdown heading markers and surrounding whitespace
    raw="$(printf '%s' "$raw" | sed -E 's/^[[:space:]]*#+[[:space:]]*//')"
    # Replace forbidden filename chars with space
    raw="$(printf '%s' "$raw" | sed -E 's/[\/\\:*?"<>|@&#]+/ /g')"
    # Collapse runs of whitespace to single space
    raw="$(printf '%s' "$raw" | tr -s '[:space:]' ' ')"
    # Trim leading/trailing whitespace
    raw="$(printf '%s' "$raw" | sed -E 's/^[[:space:]]+|[[:space:]]+$//g')"
    # Truncate to 60 chars at a word boundary if too long
    if [ "${#raw}" -gt 60 ]; then
        raw="$(printf '%s' "$raw" | cut -c1-60 | sed -E 's/[[:space:]]+[^[:space:]]*$//')"
    fi
    [ -z "$raw" ] && raw="Untitled"
    printf '%s' "$raw"
}

capture_note() {
    local title=""
    local tags=""
    local source="cli"
    local body_arg=""

    # Parse flags
    while [ $# -gt 0 ]; do
        case "$1" in
            --title)
                title="$2"
                shift 2
                ;;
            --tags)
                tags="$2"
                shift 2
                ;;
            --source)
                source="$2"
                shift 2
                ;;
            --help|-h)
                cat <<EOF
ov capture - quick-dump a note into 00-Inbox

USAGE:
    ov capture [text]                       Body from positional arg
    ov capture                              Body from stdin (when piped)
    echo "..." | ov capture --title Foo
    ov capture --title "Foo" --tags "a,b" --source llm <<EOF
    body...
    EOF

OPTIONS:
    --title <str>     Explicit title (default: derived from first line)
    --tags <a,b,c>    Comma-separated tags
    --source <name>   cli | web | llm (default: cli)
EOF
                return 0
                ;;
            --)
                shift
                body_arg="${body_arg}${body_arg:+ }$*"
                break
                ;;
            -*)
                echo -e "${RED}Unknown flag:${NC} $1" >&2
                return 1
                ;;
            *)
                # Positional body
                body_arg="${body_arg}${body_arg:+ }$1"
                shift
                ;;
        esac
    done

    # Resolve body: positional arg wins over stdin. If neither, error.
    local body=""
    if [ -n "$body_arg" ]; then
        body="$body_arg"
    elif [ ! -t 0 ]; then
        body="$(cat)"
    else
        echo -e "${RED}No content provided.${NC} Pass body as arg or pipe via stdin." >&2
        echo "Try: ov capture --help" >&2
        return 1
    fi

    # Strip trailing whitespace/newlines from body
    body="$(printf '%s' "$body" | sed -E 's/[[:space:]]+$//')"

    if [ -z "$body" ]; then
        echo -e "${RED}Empty body, refusing to capture.${NC}" >&2
        return 1
    fi

    # Auto-derive title from first non-empty line if not provided
    if [ -z "$title" ]; then
        title="$(printf '%s\n' "$body" | awk 'NF { print; exit }')"
    fi
    title="$(slugify_title "$title")"

    # Build filename: "YYYY-MM-DD HHMM <Title>.md"
    local stamp
    stamp="$(date +'%Y-%m-%d %H%M')"
    local target="$INBOX_DIR/${stamp} ${title}.md"

    # Avoid clobbering an existing file in the same minute
    if [ -e "$target" ]; then
        local n=2
        while [ -e "$INBOX_DIR/${stamp} ${title} (${n}).md" ]; do
            n=$((n+1))
        done
        target="$INBOX_DIR/${stamp} ${title} (${n}).md"
    fi

    mkdir -p "$INBOX_DIR"

    # Build frontmatter
    local created
    created="$(date +'%Y-%m-%d')"
    {
        echo "---"
        echo "type: inbox"
        echo "created: $created"
        echo "source: $source"
        if [ -n "$tags" ]; then
            # Convert "a,b,c" to YAML list "[a, b, c]"
            local yaml_tags
            yaml_tags="$(printf '%s' "$tags" | tr ',' '\n' | sed -E 's/^[[:space:]]+|[[:space:]]+$//g' | awk 'NF' | paste -sd, - | sed 's/,/, /g')"
            echo "tags: [$yaml_tags]"
        fi
        echo "---"
        echo
        echo "# $title"
        echo
        # If the body's first line is a duplicate of the title (with optional leading #s), skip it
        printf '%s\n' "$body" | awk -v t="$title" '
            BEGIN { skipped=0 }
            !skipped && NF {
                line=$0
                sub(/^[[:space:]]*#+[[:space:]]*/, "", line)
                if (line == t) { skipped=1; next }
                skipped=1
                print
                next
            }
            skipped { print }
        '
    } > "$target"

    echo -e "${GREEN}✓ Captured:${NC} ${target#$VAULT_DIR/}"
}

inbox_list() {
    echo -e "${CYAN}▸ Inbox contents${NC}"
    
    if [ ! -d "$INBOX_DIR" ]; then
        echo "  No inbox directory found"
        return
    fi
    
    local found_files=false
    for file in "$INBOX_DIR"/*.md; do
        [ -f "$file" ] || continue
        found_files=true
        
        local basename=$(basename "$file" .md)
        local age=$(get_file_age "$file")
        local age_marker=$(format_age $age)
        
        echo -e "  ${age_marker}${basename}  (${age}d old)"
    done
    
    if [ "$found_files" = false ]; then
        echo -e "  ${GREEN}✓ Inbox is empty${NC}"
    fi
}

triage_inbox() {
    echo -e "${CYAN}🔄 Triaging inbox...${NC}\n"
    
    if [ ! -d "$INBOX_DIR" ]; then
        echo "No inbox directory found"
        return
    fi
    
    for file in "$INBOX_DIR"/*.md; do
        [ -f "$file" ] || continue
        
        local filename=$(basename "$file")
        local basename="${filename%.md}"
        
        echo -e "${PURPLE}📄 $basename${NC}"
        echo "   [1] 01-Projects"
        echo "   [2] 02-Areas/Work"
        echo "   [3] 02-Areas/Learning"
        echo "   [4] 02-Areas/Personal"
        echo "   [5] 03-Resources"
        echo "   [6] 04-Archive"
        echo "   [s] Skip"
        echo "   [d] Delete"
        echo -n "   Choice: "
        
        read -r choice
        case $choice in
            1)
                mv "$file" "$PROJECTS_DIR/"
                echo -e "   ${GREEN}→ Moved to Projects${NC}\n"
                ;;
            2)
                mkdir -p "$AREAS_DIR/Work"
                mv "$file" "$AREAS_DIR/Work/"
                echo -e "   ${GREEN}→ Moved to Areas/Work${NC}\n"
                ;;
            3)
                mkdir -p "$AREAS_DIR/Learning"
                mv "$file" "$AREAS_DIR/Learning/"
                echo -e "   ${GREEN}→ Moved to Areas/Learning${NC}\n"
                ;;
            4)
                mkdir -p "$AREAS_DIR/Personal"
                mv "$file" "$AREAS_DIR/Personal/"
                echo -e "   ${GREEN}→ Moved to Areas/Personal${NC}\n"
                ;;
            5)
                mv "$file" "$RESOURCES_DIR/"
                echo -e "   ${GREEN}→ Moved to Resources${NC}\n"
                ;;
            6)
                mv "$file" "$ARCHIVE_DIR/"
                echo -e "   ${GREEN}→ Moved to Archive${NC}\n"
                ;;
            d|D)
                rm "$file"
                echo -e "   ${RED}→ Deleted${NC}\n"
                ;;
            s|S|"")
                echo -e "   ${YELLOW}→ Skipped${NC}\n"
                ;;
            *)
                echo -e "   ${YELLOW}→ Invalid choice, skipped${NC}\n"
                ;;
        esac
    done
    
    echo -e "${GREEN}✓ Triage complete${NC}"
}

new_note() {
    echo -e "${CYAN}📝 Create new note${NC}\n"
    
    echo "Note type:"
    echo "  [1] Project"
    echo "  [2] Meeting"
    echo "  [3] Learning"
    echo "  [4] General"
    echo -n "Choice: "
    read -r note_type
    
    echo -n "Title: "
    read -r title
    
    if [ -z "$title" ]; then
        echo "Title cannot be empty"
        return 1
    fi
    
    # Clean title for filename
    local filename=$(echo "$title" | sed 's/[^a-zA-Z0-9 ]//g' | sed 's/ /-/g')
    
    case $note_type in
        1)
            local template_file="$META_DIR/Project Template.md"
            local target_file="$PROJECTS_DIR/${filename}.md"
            ;;
        2)
            local template_file="$META_DIR/Meeting Note Template.md"
            local target_file="$AREAS_DIR/Work/${filename}.md"
            mkdir -p "$AREAS_DIR/Work"
            ;;
        3)
            local template_file="$META_DIR/Learning Note Template.md"
            local target_file="$AREAS_DIR/Learning/${filename}.md"
            mkdir -p "$AREAS_DIR/Learning"
            ;;
        *)
            local target_file="$INBOX_DIR/${filename}.md"
            ;;
    esac
    
    # Create note with template if available
    if [ -f "$template_file" ]; then
        cp "$template_file" "$target_file"
        # Replace title placeholder if it exists
        sed -i.bak "s/{{title}}/$title/g" "$target_file" && rm "$target_file.bak"
    else
        echo "# $title" > "$target_file"
        echo "" >> "$target_file"
    fi
    
    echo -e "${GREEN}✓ Created: $target_file${NC}"
    
    # Try to open in Obsidian
    if command -v open >/dev/null && [[ "$OSTYPE" == "darwin"* ]]; then
        open "obsidian://open?vault=main-vault&file=$(basename "$target_file" .md)"
    fi
}

review_vault() {
    echo -e "${CYAN}📊 Weekly Review${NC}\n"
    
    # Inbox count
    local inbox_count=$(find "$INBOX_DIR" -name "*.md" 2>/dev/null | wc -l)
    echo -e "${PURPLE}📥 Inbox:${NC} $inbox_count notes"
    
    # Notes modified this week
    echo -e "\n${PURPLE}📝 Modified this week:${NC}"
    find "$VAULT_DIR" -name "*.md" -mtime -7 -not -path "*/04-Archive/*" 2>/dev/null | \
        head -10 | while read -r file; do
        echo "  • $(basename "$file" .md)"
    done
    
    # Active projects
    echo -e "\n${PURPLE}🎯 Active Projects:${NC}"
    if [ -d "$PROJECTS_DIR" ]; then
        for item in "$PROJECTS_DIR"/*; do
            [ -e "$item" ] || continue
            if [ -d "$item" ]; then
                echo "  📁 $(basename "$item")"
            else
                echo "  📄 $(basename "$item" .md)"
            fi
        done
    fi
    
    # MOCs
    echo -e "\n${PURPLE}🗺 Maps of Content:${NC}"
    find "$VAULT_DIR" -name "MOC*.md" 2>/dev/null | while read -r moc; do
        echo "  🗺 $(basename "$moc" .md)"
    done
    
    echo -e "\n${GREEN}💡 Next steps:${NC}"
    echo "  • Process inbox with 'ov triage'"
    echo "  • Check for stale notes with 'ov stale'"
    echo "  • Update brag document if work-related wins"
}

find_stale() {
    local days=${1:-90}
    echo -e "${CYAN}🔍 Notes not touched in $days+ days${NC}\n"
    
    find "$VAULT_DIR" -name "*.md" -mtime +$days \
        -not -path "*/04-Archive/*" \
        -not -path "*/99-Meta/*" \
        -not -path "*/Daily Notes/*" \
        2>/dev/null | while read -r file; do
        local age=$(get_file_age "$file")
        local rel_path=${file#$VAULT_DIR/}
        echo "  📄 $rel_path (${age}d old)"
    done
}

# MOC functions
moc_list() {
    echo -e "${CYAN}🗺 Maps of Content${NC}\n"
    
    find "$VAULT_DIR" -name "MOC*.md" 2>/dev/null | while read -r moc; do
        local name=$(basename "$moc" .md)
        echo -e "  ${PURPLE}$name${NC}"
        
        # Try to extract description from first few lines
        head -5 "$moc" | grep -v "^#" | grep -v "^$" | head -1 | sed 's/^/    /'
        echo
    done
}

moc_new() {
    echo -e "${CYAN}📝 Create new MOC${NC}\n"
    
    echo -n "MOC title (e.g., 'Travel', 'Home Improvement'): "
    read -r title
    
    if [ -z "$title" ]; then
        echo "Title cannot be empty"
        return 1
    fi
    
    local filename="MOC ${title}.md"
    local target_file="$RESOURCES_DIR/$filename"
    
    cat > "$target_file" << EOF
# MOC $title

> Map of Content for $title - links to all related notes and resources

## Overview

## Key Notes

## Resources

## Related MOCs

---
*Created: $(date +%Y-%m-%d)*
EOF
    
    echo -e "${GREEN}✓ Created: $target_file${NC}"
    
    # Try to open in Obsidian
    if command -v open >/dev/null && [[ "$OSTYPE" == "darwin"* ]]; then
        open "obsidian://open?vault=main-vault&file=$filename"
    fi
}

moc_orphan() {
    echo -e "${CYAN}🔍 Finding orphaned notes...${NC}\n"
    
    # Get all MOC files
    local mocs=$(find "$VAULT_DIR" -name "MOC*.md" 2>/dev/null)
    
    # Check notes in Resources and Areas
    for dir in "$RESOURCES_DIR" "$AREAS_DIR"; do
        [ -d "$dir" ] || continue
        
        find "$dir" -name "*.md" 2>/dev/null | while read -r note; do
            local note_name=$(basename "$note" .md)
            local is_moc=false
            
            # Skip if this is itself a MOC
            if [[ "$note_name" == MOC* ]]; then
                continue
            fi
            
            # Check if mentioned in any MOC
            local found_in_moc=false
            echo "$mocs" | while read -r moc; do
                [ -f "$moc" ] || continue
                if grep -q "$note_name" "$moc" 2>/dev/null; then
                    found_in_moc=true
                    break
                fi
            done
            
            if [ "$found_in_moc" = false ]; then
                local rel_path=${note#$VAULT_DIR/}
                echo "  📄 $rel_path"
            fi
        done
    done
}

moc_add() {
    echo -e "${CYAN}🔗 Add note to MOC${NC}\n"
    
    # List available MOCs
    echo "Available MOCs:"
    local mocs=($(find "$VAULT_DIR" -name "MOC*.md" 2>/dev/null))
    
    if [ ${#mocs[@]} -eq 0 ]; then
        echo "No MOCs found. Create one with 'ov mocs new'"
        return 1
    fi
    
    for i in "${!mocs[@]}"; do
        local name=$(basename "${mocs[$i]}" .md)
        echo "  [$((i+1))] $name"
    done
    
    echo -n "Choose MOC: "
    read -r moc_choice
    
    if ! [[ "$moc_choice" =~ ^[0-9]+$ ]] || [ "$moc_choice" -lt 1 ] || [ "$moc_choice" -gt ${#mocs[@]} ]; then
        echo "Invalid choice"
        return 1
    fi
    
    local selected_moc="${mocs[$((moc_choice-1))]}"
    
    echo -n "Note name to add: "
    read -r note_name
    
    if [ -z "$note_name" ]; then
        echo "Note name cannot be empty"
        return 1
    fi
    
    # Add to MOC under "Key Notes" section
    if grep -q "## Key Notes" "$selected_moc"; then
        sed -i.bak "/## Key Notes/a\\
- [[$note_name]]" "$selected_moc" && rm "$selected_moc.bak"
    else
        echo "- [[$note_name]]" >> "$selected_moc"
    fi
    
    echo -e "${GREEN}✓ Added '$note_name' to $(basename "$selected_moc")${NC}"
}

# publish_doc: optionally convert a .md note to HTML via LLM, then rsync to docs host
publish_doc() {
    local file=""
    local use_llm=0
    local llm_desc=""

    while [ $# -gt 0 ]; do
        case "$1" in
            --llm)       use_llm=1; shift ;;
            --desc)      llm_desc="$2"; shift 2 ;;
            -*)          echo -e "${RED}Unknown flag: $1${NC}" >&2; return 1 ;;
            *)           file="$1"; shift ;;
        esac
    done

    local host="${OV_DOCS_HOST:-}"
    local remote_path="${OV_DOCS_PATH:-/var/www/docs}"
    local url_base="${OV_DOCS_URL:-}"

    if [ -z "$host" ]; then
        echo -e "${RED}OV_DOCS_HOST not set.${NC}" >&2
        echo "    Add OV_DOCS_HOST=your-server to ~/.config/ov/config" >&2
        return 1
    fi

    # Interactive picker when no file given
    if [ -z "$file" ]; then
        if ! command -v gum &>/dev/null; then
            echo -e "${RED}gum not found — pass a file path or install gum.${NC}" >&2
            return 1
        fi
        file=$(find "$VAULT_DIR" -name "*.md" -not -path "*/.obsidian/*" \
               | sort \
               | gum filter \
                   --prompt="Publish note > " \
                   --height=70%)
        [ -z "$file" ] && return 0  # cancelled
    fi

    if [ ! -f "$file" ]; then
        echo -e "${RED}File not found: $file${NC}" >&2
        return 1
    fi

    local ext="${file##*.}"
    local publish_file="$file"

    # .md without --llm: bail with a hint
    if [ "$ext" = "md" ] && [ "$use_llm" -eq 0 ]; then
        echo -e "${YELLOW}⚠  That's a markdown file.${NC}"
        echo    "   Use --llm to convert it to HTML first:"
        echo    "   ov publish \"$file\" --llm"
        return 1
    fi

    # --llm: convert .md → styled HTML
    if [ "$use_llm" -eq 1 ]; then
        if [ "$ext" != "md" ]; then
            echo -e "${YELLOW}⚠  --llm ignored: file is already .${ext}, publishing as-is.${NC}"
        else
            local llm_cmd="${OV_LLM_CMD:-claude --print}"
            local guidance="${llm_desc:-clean, modern design with good typography and readable line lengths}"
            local slug
            slug=$(basename "$file" .md | tr '[:upper:]' '[:lower:]' | tr ' ' '-' | tr -cd '[:alnum:]-')
            local out_dir="$VAULT_DIR/Published"
            mkdir -p "$out_dir"
            local out_file="$out_dir/${slug}.html"

            echo -e "${CYAN}🤖 Converting with LLM...${NC}"
            {
                echo "Convert this Obsidian markdown note into a complete, self-contained HTML file."
                echo "Design guidance: ${guidance}"
                echo "Rules: single file, inline all CSS and JS, no external dependencies."
                echo "Return ONLY the HTML — no markdown, no code fences, no explanation."
                echo ""
                echo "---"
                cat "$file"
            } | eval "$llm_cmd" > "$out_file"

            echo -e "${GREEN}✓ HTML saved: ${out_file}${NC}"
            publish_file="$out_file"
        fi
    fi

    local filename
    filename="$(basename "$publish_file")"

    echo -e "${CYAN}📤 Publishing ${filename}...${NC}"
    rsync -avz "$publish_file" "${host}:${remote_path}/"

    if [ -n "$url_base" ]; then
        echo -e "\n${GREEN}✓ Live at: ${url_base}/${filename}${NC}"
    else
        echo -e "\n${GREEN}✓ Published to ${host}:${remote_path}/${filename}${NC}"
    fi
}

# unpublish_doc: remove one or more files from the docs server
unpublish_doc() {
    local host="${OV_DOCS_HOST:-}"
    local remote_path="${OV_DOCS_PATH:-/var/www/docs}"
    local url_base="${OV_DOCS_URL:-}"

    if [ -z "$host" ]; then
        echo -e "${RED}OV_DOCS_HOST not set.${NC}" >&2
        return 1
    fi

    # Direct removal if filenames passed as args
    if [ $# -gt 0 ]; then
        for f in "$@"; do
            local base
            base=$(basename "$f")
            echo -e "${CYAN}🗑  Removing ${base}...${NC}"
            ssh "$host" "rm -f '${remote_path}/${base}'"
            echo -e "${GREEN}✓ Removed${NC}"
        done
        return 0
    fi

    # Interactive multi-select picker
    if ! command -v gum &>/dev/null; then
        echo -e "${RED}gum not found — pass filename(s) directly or install gum.${NC}" >&2
        return 1
    fi

    echo -e "${CYAN}Fetching published files...${NC}"
    local remote_files
    remote_files=$(ssh "$host" "ls -1 '${remote_path}/' 2>/dev/null")

    if [ -z "$remote_files" ]; then
        echo -e "${YELLOW}No files on docs server.${NC}"
        return 0
    fi

    local selected
    selected=$(echo "$remote_files" | gum filter --no-limit \
        --prompt="Unpublish > " \
        --height=50%)

    [ -z "$selected" ] && { echo -e "${YELLOW}Cancelled.${NC}"; return 0; }

    echo -e "${YELLOW}Will remove:${NC}"
    echo "$selected" | while read -r f; do echo "  • $f"; done
    echo -n "Confirm? [y/N] "
    read -r confirm
    [ "$confirm" != "y" ] && [ "$confirm" != "Y" ] && { echo -e "${YELLOW}Cancelled.${NC}"; return 0; }

    echo "$selected" | while read -r f; do
        echo -e "${CYAN}🗑  Removing ${f}...${NC}"
        ssh "$host" "rm -f '${remote_path}/${f}'"
        echo -e "${GREEN}✓ Removed${NC}"
    done
}

# Main command handler
case "${1:-help}" in
    inbox)
        inbox_list
        ;;
    capture)
        shift
        capture_note "$@"
        ;;
    triage)
        shift
        if [ "${1:-}" = "--llm" ]; then
            shift
            # triage_llm.py lives next to this script in the repo's bin/ dir
            python3 "$SCRIPT_DIR/triage_llm.py" --vault "$VAULT_DIR" "$@"
        else
            triage_inbox
        fi
        ;;
    new)
        new_note
        ;;
    review)
        review_vault
        ;;
    stale)
        find_stale "$2"
        ;;
    mocs)
        case "${2:-list}" in
            list)
                moc_list
                ;;
            new)
                moc_new
                ;;
            orphan)
                moc_orphan
                ;;
            add)
                moc_add
                ;;
            update)
                echo "MOC update functionality coming soon"
                ;;
            *)
                echo "Unknown mocs command: $2"
                show_help
                exit 1
                ;;
        esac
        ;;
    publish)
        shift
        publish_doc "$@"
        ;;
    unpublish)
        shift
        unpublish_doc "$@"
        ;;
    help|--help|-h)
        show_help
        ;;
    *)
        echo "Unknown command: $1"
        show_help
        exit 1
        ;;
esac