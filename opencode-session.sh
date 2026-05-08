#!/usr/bin/env bash
set -euo pipefail

DB="$HOME/.local/share/opencode/opencode.db"

if [ ! -f "$DB" ]; then
  echo "Error: opencode database not found at $DB" >&2
  exit 1
fi

if ! command -v sqlite3 &>/dev/null; then
  echo "Error: sqlite3 is required" >&2
  exit 1
fi

list_sessions() {
  local limit=${1:-30}
  sqlite3 "$DB" -separator " │ " \
    "SELECT s.id, s.slug, substr(s.title,1,50),
            datetime(s.time_created/1000, 'unixepoch') as created,
            coalesce(p.name, s.project_id) as project
     FROM session s
     LEFT JOIN project p ON p.id = s.project_id
     ORDER BY s.time_created DESC
     LIMIT $limit;"
  echo ""
  echo "Usage: opencode-session.sh [--limit N] [session-id]"
  exit 0
}

show_session() {
  local sid="$1"

  local row
  row=$(sqlite3 "$DB" -json \
    "SELECT s.id, s.slug, s.title, s.directory, s.version,
            s.summary_additions, s.summary_deletions, s.summary_files,
            s.summary_diffs, s.share_url, s.revert, s.permission,
            s.time_created, s.time_updated, s.time_compacting,
            s.agent, s.model,
            coalesce(p.name, s.project_id) as project_name,
            p.worktree as project_path,
            (SELECT COUNT(*) FROM message WHERE session_id = s.id) as message_count,
            (SELECT COUNT(*) FROM todo WHERE session_id = s.id) as todo_count,
            (SELECT COUNT(*) FROM todo WHERE session_id = s.id AND status = 'completed') as todos_done
     FROM session s
     LEFT JOIN project p ON p.id = s.project_id
     WHERE s.id = '$sid';" 2>/dev/null || true)

  if [ -z "$row" ] || [ "$row" = "[]" ]; then
    echo "Session not found: $sid"
    exit 1
  fi

  local slug title pname ptime_created ptime_updated ptime_compacting
  local additions deletions files diffs share_url agent model
  local msg_count todo_count todos_done

  slug=$(echo "$row" | python3 -c "import json,sys; print(json.load(sys.stdin)[0].get('slug',''))")
  title=$(echo "$row" | python3 -c "import json,sys; print(json.load(sys.stdin)[0].get('title',''))")
  pname=$(echo "$row" | python3 -c "import json,sys; print(json.load(sys.stdin)[0].get('project_name',''))")
  ptime_created=$(echo "$row" | python3 -c "import json,sys; print(json.load(sys.stdin)[0].get('time_created','') or '')")
  ptime_updated=$(echo "$row" | python3 -c "import json,sys; print(json.load(sys.stdin)[0].get('time_updated','') or '')")
  ptime_compacting=$(echo "$row" | python3 -c "import json,sys; print(json.load(sys.stdin)[0].get('time_compacting','') or '')")
  additions=$(echo "$row" | python3 -c "import json,sys; print(json.load(sys.stdin)[0].get('summary_additions','') or '')")
  deletions=$(echo "$row" | python3 -c "import json,sys; print(json.load(sys.stdin)[0].get('summary_deletions','') or '')")
  files=$(echo "$row" | python3 -c "import json,sys; print(json.load(sys.stdin)[0].get('summary_files','') or '')")
  diffs=$(echo "$row" | python3 -c "import json,sys; print(json.load(sys.stdin)[0].get('summary_diffs','') or '')")
  share_url=$(echo "$row" | python3 -c "import json,sys; print(json.load(sys.stdin)[0].get('share_url','') or '')")
  agent=$(echo "$row" | python3 -c "import json,sys; print(json.load(sys.stdin)[0].get('agent','') or '')")
  model=$(echo "$row" | python3 -c "
import json,sys
m = json.load(sys.stdin)[0].get('model','')
if m:
    try: print(json.loads(m).get('id',''))
    except: print(m)
else: print('')")
  msg_count=$(echo "$row" | python3 -c "import json,sys; print(json.load(sys.stdin)[0].get('message_count',0))")
  todo_count=$(echo "$row" | python3 -c "import json,sys; print(json.load(sys.stdin)[0].get('todo_count',0))")
  todos_done=$(echo "$row" | python3 -c "import json,sys; print(json.load(sys.stdin)[0].get('todos_done',0))")

  fmt_ts() {
    local ms=$1
    if [ -z "$ms" ] || [ "$ms" = "0" ]; then echo "N/A"; return; fi
    python3 -c "from datetime import datetime, timezone; print(datetime.fromtimestamp($ms/1000, tz=timezone.utc).strftime('%Y-%m-%d %H:%M:%S UTC'))"
  }

  echo "Session:       $sid"
  echo "Slug:          $slug"
  echo "Title:         $title"
  echo "Project:       $pname"
  echo "Agent:         ${agent:-N/A}"
  echo "Model:         ${model:-N/A}"
  echo "Created:       $(fmt_ts "$ptime_created")"
  echo "Updated:       $(fmt_ts "$ptime_updated")"
  echo ""

  local compacted
  compacted=$(fmt_ts "$ptime_compacting")
  echo "Stats:"
  echo "  Messages:    $msg_count"
  echo "  Todos:       $todo_count ($todos_done done)"
  [ -n "$additions" ] && echo "  Additions:   $additions" || true
  [ -n "$deletions" ] && echo "  Deletions:   $deletions" || true
  [ -n "$files" ]     && echo "  Files:       $files" || true
  [ "$compacted" != "N/A" ] && echo "  Compacted:   $compacted" || true
  [ -n "$share_url" ] && echo "  Share URL:   $share_url" || true
  [ -n "$diffs" ]     && echo "  Diffs:       $diffs" || true
  echo ""

  echo "── Messages ──"
  sqlite3 "$DB" -separator " │ " \
    "SELECT datetime(time_created/1000, 'unixepoch'),
            substr(replace(data, char(10), ' '), 1, 90)
     FROM message
     WHERE session_id = '$sid'
     ORDER BY time_created DESC
     LIMIT 10;" 2>/dev/null || echo "  (none)"

  local tcount
  tcount=$(sqlite3 "$DB" "SELECT COUNT(*) FROM todo WHERE session_id = '$sid';" 2>/dev/null || echo 0)
  if [ "$tcount" -gt 0 ]; then
    echo ""
    echo "── Todos ──"
    sqlite3 "$DB" -separator " │ " \
      "SELECT content, status, priority
       FROM todo
       WHERE session_id = '$sid'
       ORDER BY position;" 2>/dev/null
  fi
}

LIMIT=30
SID=""

while [ $# -gt 0 ]; do
  case "$1" in
    --limit|-n)
      shift
      LIMIT="${1:-30}"
      shift
      ;;
    -*)
      echo "Unknown option: $1" >&2
      echo "Usage: opencode-session.sh [--limit N] [session-id]" >&2
      exit 1
      ;;
    *)
      SID="$1"
      shift
      ;;
  esac
done

if [ -z "$SID" ]; then
  list_sessions "$LIMIT"
else
  show_session "$SID"
fi
