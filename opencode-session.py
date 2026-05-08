#!/usr/bin/env python3
import sqlite3, json, sys, os
from datetime import datetime, timezone

DB = os.path.expanduser("~/.local/share/opencode/opencode.db")
LIMIT = 30


def fmt_ts(ms):
    if not ms:
        return "N/A"
    return datetime.fromtimestamp(ms / 1000, tz=timezone.utc).strftime(
        "%Y-%m-%d %H:%M:%S UTC"
    )


def list_sessions(limit):
    conn = sqlite3.connect(DB)
    cur = conn.execute(
        """SELECT s.id, s.slug, substr(s.title,1,50),
                  datetime(s.time_created/1000, 'unixepoch') as created,
                  coalesce(p.name, s.project_id) as project
           FROM session s
           LEFT JOIN project p ON p.id = s.project_id
           ORDER BY s.time_created DESC
           LIMIT ?""",
        (limit,),
    )
    for row in cur:
        print(" │ ".join(str(c) for c in row))
    print()
    print("Usage: opencode-session [--limit N] [session-id]")


def show_session(sid):
    conn = sqlite3.connect(DB)
    conn.row_factory = sqlite3.Row

    row = conn.execute(
        """SELECT s.*,
                  coalesce(p.name, s.project_id) as project_name,
                  (SELECT COUNT(*) FROM message WHERE session_id = s.id) as message_count,
                  (SELECT COUNT(*) FROM todo WHERE session_id = s.id) as todo_count,
                  (SELECT COUNT(*) FROM todo WHERE session_id = s.id AND status = 'completed') as todos_done
           FROM session s
           LEFT JOIN project p ON p.id = s.project_id
           WHERE s.id = ?""",
        (sid,),
    ).fetchone()

    if not row:
        print(f"Session not found: {sid}", file=sys.stderr)
        sys.exit(1)

    model = row["model"]
    if model:
        try:
            model = json.loads(model).get("id", model)
        except json.JSONDecodeError:
            pass

    compacted = fmt_ts(row["time_compacting"])

    print(f"Session:       {sid}")
    print(f"Slug:          {row['slug']}")
    print(f"Title:         {row['title']}")
    print(f"Project:       {row['project_name']}")
    print(f"Agent:         {row['agent'] or 'N/A'}")
    print(f"Model:         {model or 'N/A'}")
    print(f"Created:       {fmt_ts(row['time_created'])}")
    print(f"Updated:       {fmt_ts(row['time_updated'])}")
    print()
    print("Stats:")
    print(f"  Messages:    {row['message_count']}")
    print(f"  Todos:       {row['todo_count']} ({row['todos_done']} done)")
    if row["summary_additions"]:
        print(f"  Additions:   {row['summary_additions']}")
    if row["summary_deletions"]:
        print(f"  Deletions:   {row['summary_deletions']}")
    if row["summary_files"]:
        print(f"  Files:       {row['summary_files']}")
    if compacted != "N/A":
        print(f"  Compacted:   {compacted}")
    if row["share_url"]:
        print(f"  Share URL:   {row['share_url']}")
    if row["summary_diffs"]:
        print(f"  Diffs:       {row['summary_diffs']}")
    print()

    print("── Messages ──")
    msg_rows = conn.execute(
        """SELECT id, data FROM message
           WHERE session_id = ?
           ORDER BY time_created DESC
           LIMIT 10""",
        (sid,),
    ).fetchall()

    if msg_rows:
        for mrow in msg_rows:
            msg = json.loads(mrow["data"])
            role = msg.get("role", "?")
            ts = msg.get("time", {}).get("created", "")

            parts = conn.execute(
                "SELECT data FROM part WHERE message_id = ? ORDER BY time_created",
                (mrow["id"],),
            ).fetchall()
            parts_data = [json.loads(r["data"]) for r in parts]

            if role == "user":
                texts = [
                    p.get("text", "")
                    for p in parts_data
                    if p.get("type") == "text"
                ]
                text = texts[0] if texts else ""
                preview = (
                    text.strip().replace("\n", " ")[:90]
                    if text
                    else "(no text)"
                )
                print(f"  [{role}] {preview}")
            else:
                info = []
                for p in parts_data:
                    ptype = p.get("type")
                    if ptype == "text":
                        t = p.get("text", "").strip().replace("\n", " ")[:90]
                        info.append(f"text: {t}" if t else "text")
                    elif ptype == "reasoning":
                        t = p.get("text", "").strip().replace("\n", " ")[:60]
                        info.append(f"reasoning: {t}" if t else "reasoning")
                    elif ptype == "tool":
                        info.append(f"tool:{p.get('tool', '?')}")
                    elif ptype == "step-start":
                        info.append("step-start")
                    elif ptype == "tool-result":
                        info.append("tool-result")
                finish = msg.get("finish", "")
                if finish:
                    info.append(f"finish={finish}")
                print(f"  [{role}] {' | '.join(info)}")
    else:
        print("  (none)")

    todo_rows = conn.execute(
        "SELECT content, status, priority FROM todo WHERE session_id = ? ORDER BY position",
        (sid,),
    ).fetchall()

    if todo_rows:
        print()
        print("── Todos ──")
        for t in todo_rows:
            print(f"{t['content']} │ {t['status']} │ {t['priority']}")


if __name__ == "__main__":
    limit = LIMIT
    sid = None

    args = sys.argv[1:]
    while args:
        arg = args.pop(0)
        if arg in ("--limit", "-n"):
            try:
                limit = int(args.pop(0)) if args else LIMIT
            except (ValueError, IndexError):
                limit = LIMIT
        elif arg.startswith("-"):
            print(f"Unknown option: {arg}", file=sys.stderr)
            print("Usage: opencode-session [--limit N] [session-id]", file=sys.stderr)
            sys.exit(1)
        else:
            sid = arg

    if not os.path.exists(DB):
        print(f"Error: opencode database not found at {DB}", file=sys.stderr)
        sys.exit(1)

    if sid:
        show_session(sid)
    else:
        list_sessions(limit)
