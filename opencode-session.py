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


def search_sessions(term):
    conn = sqlite3.connect(DB)
    conn.row_factory = sqlite3.Row

    seen = {}

    title_rows = conn.execute(
        """SELECT s.id, s.title, datetime(s.time_created/1000, 'unixepoch') as created
           FROM session s
           WHERE s.title LIKE '%' || ? || '%'
           ORDER BY s.time_created DESC
           LIMIT 30""",
        (term,),
    ).fetchall()
    for r in title_rows:
        seen[r["id"]] = {
            "id": r["id"],
            "title": r["title"],
            "created": r["created"],
            "match": "Title matches",
            "preview": (r["title"] or "")[:60],
        }

    content_rows = conn.execute(
        """SELECT DISTINCT s.id, s.title,
                  datetime(s.time_created/1000, 'unixepoch') as created,
                  json_extract(p.data, '$.text') as full_text
           FROM session s
           JOIN message m ON m.session_id = s.id
           JOIN part p ON p.message_id = m.id
           WHERE json_extract(p.data, '$.type') = 'text'
             AND json_extract(p.data, '$.text') LIKE '%' || ? || '%'
           ORDER BY s.time_created DESC
           LIMIT 30""",
        (term,),
    ).fetchall()
    for r in content_rows:
        if r["id"] not in seen:
            text = r["full_text"] or ""
            pos = text.lower().find(term.lower())
            if pos >= 0:
                start = max(0, pos - 25)
                end = min(len(text), pos + len(term) + 25)
                preview = text[start:end].replace("\n", " ").strip()
                if start > 0:
                    preview = "..." + preview
                if end < len(text):
                    preview = preview + "..."
            else:
                preview = (text[:60].replace("\n", " ") + "...") if text else ""
            seen[r["id"]] = {
                "id": r["id"],
                "title": r["title"],
                "created": r["created"],
                "match": "Content matches",
                "preview": preview,
            }

    if not seen:
        print(f'No sessions found matching: {term}')
        return

    print(f'── Search Results for "{term}" ──')
    for sid in seen:
        r = seen[sid]
        print(f'  {r["id"]} │ {r["created"]} │ {r["match"]}: "{r["preview"]}"')


def list_sessions(limit):
    conn = sqlite3.connect(DB)

    parents = conn.execute(
        """SELECT s.id, s.slug, s.agent, substr(s.title,1,50) as title,
                  datetime(s.time_created/1000, 'unixepoch') as created,
                  coalesce(p.name, s.project_id) as project
           FROM session s
           LEFT JOIN project p ON p.id = s.project_id
           WHERE s.parent_id IS NULL
           ORDER BY s.time_created DESC
           LIMIT ?""",
        (limit,),
    ).fetchall()

    if not parents:
        return

    parent_ids = [r[0] for r in parents]
    children_by_parent = {}

    if parent_ids:
        placeholders = ",".join("?" * len(parent_ids))
        children = conn.execute(
            f"""SELECT s.id, s.slug, s.agent, substr(s.title,1,50) as title,
                       datetime(s.time_created/1000, 'unixepoch') as created,
                       coalesce(p.name, s.project_id) as project,
                       s.parent_id
                FROM session s
                LEFT JOIN project p ON p.id = s.project_id
                WHERE s.parent_id IN ({placeholders})
                ORDER BY s.time_created ASC""",
            parent_ids,
        ).fetchall()
        for child in children:
            children_by_parent.setdefault(child[6], []).append(child)

    print(f"{'SESSION':<24} {'SLUG':<18} {'AGENT':<9} {'CREATED':<22} PROJECT")
    print("-" * 100)

    for parent in parents:
        pid, slug, agent, title, created, project = parent
        agent_d = agent or "-"
        print(f"{pid:<24} {slug:<18} {agent_d:<9} {created:<22} {project}")
        for child in children_by_parent.get(pid, []):
            cid, cslug, cagent, ctitle, ccreated, cproject = child[:6]
            cagent_d = cagent or "-"
            print(f"  {cid:<22} {cslug:<18} {cagent_d:<9} {ccreated:<22} {cproject}")

    print()
    print("Usage: opencode-session [--limit N] [--search TERM|-s TERM] [session-id]")


def forensic_stats(conn, sid):
    print("── Forensic Stats ──")

    tool_rows = conn.execute(
        """SELECT json_extract(p.data, '$.tool') as tool_name, COUNT(*) as cnt
           FROM part p
           JOIN message m ON m.id = p.message_id
           WHERE m.session_id = ? AND json_extract(p.data, '$.type') = 'tool'
           GROUP BY 1 ORDER BY cnt DESC""",
        (sid,),
    ).fetchall()
    if tool_rows:
        print("  Tool Usage:")
        for r in tool_rows:
            print(f"    {r['tool_name']:<22} {r['cnt']}")
        print()

    part_rows = conn.execute(
        """SELECT json_extract(p.data, '$.type') as ptype, COUNT(*) as cnt
           FROM part p
           JOIN message m ON m.id = p.message_id
           WHERE m.session_id = ?
           GROUP BY 1 ORDER BY cnt DESC""",
        (sid,),
    ).fetchall()
    if part_rows:
        print("  Part Types:")
        for r in part_rows:
            print(f"    {r['ptype']:<22} {r['cnt']}")
        print()

    finish_rows = conn.execute(
        """SELECT json_extract(m.data, '$.finish') as finish, COUNT(*) as cnt
           FROM message m
           WHERE m.session_id = ? AND json_extract(m.data, '$.role') = 'assistant'
           GROUP BY 1""",
        (sid,),
    ).fetchall()
    if finish_rows:
        print("  Finish Reasons:")
        for r in finish_rows:
            print(f"    {r['finish'] or 'none':<22} {r['cnt']}")
        print()


def show_session(sid, show_all=False):
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

    if row["parent_id"]:
        prow = conn.execute(
            "SELECT id, slug, agent, title FROM session WHERE id = ?",
            (row["parent_id"],),
        ).fetchone()
        if prow:
            print(f"Parent:        {prow['id']} ({prow['slug']}, {prow['agent'] or '?'})")
            print()
    else:
        child_rows = conn.execute(
            """SELECT id, slug, agent, title,
                      datetime(time_created/1000, 'unixepoch') as created
               FROM session
               WHERE parent_id = ?
               ORDER BY time_created ASC""",
            (sid,),
        ).fetchall()
        if child_rows:
            print("Subagents:")
            for cr in child_rows:
                print(f"  {cr['id']}  {cr['slug']:<20} {cr['agent'] or '-':<10} {cr['created']}")
            print()

    print("── Messages ──")
    msg_rows = conn.execute(
        f"""SELECT id, data FROM message
            WHERE session_id = ?
            ORDER BY time_created {'ASC' if show_all else 'DESC'}
            {'LIMIT 10' if not show_all else ''}""",
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
                mid_short = mrow["id"][-8:]
                print(f"  [{role}:{mid_short}] {preview}")
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
                mid_short = mrow["id"][-8:]
                print(f"  [{role}:{mid_short}] {' | '.join(info)}")
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

    print()
    forensic_stats(conn, sid)


if __name__ == "__main__":
    limit = LIMIT
    sid = None
    show_all = False
    search_term = None

    args = sys.argv[1:]
    while args:
        arg = args.pop(0)
        if arg in ("--limit", "-n"):
            try:
                limit = int(args.pop(0)) if args else LIMIT
            except (ValueError, IndexError):
                limit = LIMIT
        elif arg in ("--all", "-a"):
            show_all = True
        elif arg in ("--search", "-s"):
            search_term = args.pop(0) if args else None
            if not search_term:
                print("Error: --search requires a term", file=sys.stderr)
                sys.exit(1)
        elif arg.startswith("-"):
            print(f"Unknown option: {arg}", file=sys.stderr)
            print("Usage: opencode-session [--limit N] [--all] [--search TERM|-s TERM] [session-id]", file=sys.stderr)
            sys.exit(1)
        else:
            sid = arg

    if not os.path.exists(DB):
        print(f"Error: opencode database not found at {DB}", file=sys.stderr)
        sys.exit(1)

    if search_term:
        search_sessions(search_term)
    elif sid:
        show_session(sid, show_all)
    else:
        list_sessions(limit)
