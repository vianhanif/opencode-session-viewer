# opencode-session-viewer

CLI tool to inspect [opencode](https://opencode.ai) session metadata from the local SQLite DB.

**Language:** Go  
**Source:** `main.go`  
**Binary:** `opencode-session`  
**Symlink:** `~/.local/bin/opencode-session`

## Quick start

```bash
# build
go build -o opencode-session .

# link the binary
ln -sf "$(pwd)/opencode-session" ~/.local/bin/opencode-session

# list recent sessions
opencode-session

# show session details
opencode-session ses_xxx

# list more/less sessions
opencode-session --limit 50
opencode-session -n 5
```

---

## 1. What

A CLI tool that queries opencode's local SQLite DB (`~/.local/share/opencode/opencode.db`) to inspect session metadata — title, project, model, message count, todo progress, diff stats, and recent messages with their actual content.

---

## 2. Why

### Comparison with `opencode session list`

The built-in `opencode session list` shows only **3 fields**: Session ID, Title, and last Updated time.

| Data | `opencode session list` | `opencode-session list` | `opencode-session detail` |
|------|------------------------|--------------------------|---------------------------|
| Session ID | ✅ | ✅ | ✅ |
| Title | ✅ | ✅ | ✅ |
| Updated time | ✅ | ❌ | ✅ |
| Agent used | ❌ | ✅ | ✅ |
| Model used | ❌ | ✅ | ✅ |
| Created time | ❌ | ✅ | ✅ |
| Slug | ❌ | ❌ | ✅ |
| Project name / path | ❌ | ❌ | ✅ |
| Message count | ❌ | ❌ | ✅ |
| Todo list with status | ❌ | ❌ | ✅ |
| Recent messages | ❌ | ❌ | ✅ |

There is **no built-in detail command** — `opencode session` only has `list` and `delete`. To get the above fields, you'd need to run `opencode db` with raw SQL or `opencode export` and parse JSON manually.

This tool fills that gap for:

- Checking what model/agent a session used
- Seeing todo progress without reopening a session
- Finding a session ID (by listing recent ones)
- Quick audit of what work happened across sessions
- Debugging (e.g., did a session compact? how many messages?)

---

## 3. How

### Data Source

`~/.local/share/opencode/opencode.db` — SQLite database with tables:

| Table | Role |
|-------|------|
| `session` | Per-session metadata (id, title, project, model, timestamps, diff summary) |
| `message` | Message metadata linked to session (role, agent, tokens, finish reason) |
| `part` | Actual conversation content linked to message (user text, assistant reasoning, tool calls/results) |
| `todo` | Todo items per session (content, status, priority) |
| `project` | Registered projects (id, name, worktree path) |

### Query Logic

1. **List mode** (no args or `--limit N`): Queries N top-level (parent) sessions ordered by `time_created DESC`, then fetches all their subagent children in a second query. Children are grouped and indented under their parent. Displays columns: SESSION, TITLE, AGENT, MODEL, CREATED.
2. **Detail mode** (session ID arg): Fetches the session row + counts for messages/todos via subqueries. Also shows parent session link (if a subagent) or subagent child list (if a parent). Parses the JSON `model` field to extract the model ID. Formats timestamps (stored as ms epoch) to UTC.
3. **Message rendering**: Fetches last 10 messages, then for each looks up its `part` rows to extract the actual text (user prompts, assistant reasoning, responses, tool calls). Parts are grouped by message and rendered by type — `text` (user/assistant messages), `reasoning` (thinking), `tool` (invoked tool name), `tool-result`, etc.

### Invocation

| Method | Command |
|--------|---------|
| Terminal | `opencode-session [--limit N] [--search TERM\|-s TERM] [session-id]` |
| opencode TUI (raw output) | `!opencode-session ses_xxx` |
| opencode TUI (LLM-assisted) | `/session ses_xxx` |

### Commands

| Command | Mode | Description |
|---------|------|-------------|
| `opencode-session` | List | Show last 30 parent sessions with subagents grouped under them (SESSION, TITLE, AGENT, MODEL, CREATED) |
| `opencode-session --limit 100` | List | Show last N parent sessions (children always included) |
| `opencode-session --search "term"` | Search | Search sessions by title or message content, same table format |
| `opencode-session -s "term"` | Search | Shorthand for `--search` |
| `opencode-session ses_xxx` | Detail | Show full session details, messages, todos, and forensic stats |
| `opencode-session ses_xxx --all` | Detail | Same as above, but shows all messages chronologically (oldest first) |
| `opencode-session -n 50` | List | `-n` shorthand for `--limit` |

---

## 4. Use Cases

### From within an active opencode session

While in an opencode TUI session, you can inspect any past session without leaving the TUI:

```
!opencode-session ses_1fd16538dffe2k04ZI09RKHDBT
```

This pipes the raw output into your current conversation's context, so the LLM can reference it (e.g., "continue from where that session left off", "apply the same approach here", "what todos were pending?").

### Practical scenarios

| Use Case | How |
|----------|-----|
| Resume abandoned work | Find the session ID, then `!opencode-session ses_xxx` to remind yourself of the todos and last messages |
| Audit teammate sessions | List sessions, check what model/agent was used and what files were changed |
| Debug compaction issues | Check `time_compacting` — if absent, session never compacted; if very recent, might explain context loss |
| Track completion rate | `opencode-session` shows todos_done / total — useful for standup or progress reviews |
| Verify model usage | Confirm a session used the intended model (e.g., `kimi-k2.5` vs `big-pickle`) — visible directly in list view |
| Pre-populate a new prompt | `!opencode-session ses_xxx` inside the TUI gives the LLM full context of prior work without manual copy-paste |

### Limitations

- Read-only — never writes to the DB
- Depends on Go runtime for building; binary is standalone after build
- Model field parsing is best-effort (some sessions store raw string, others JSON)
- Content search (`--search`) uses `JSON_EXTRACT` + `LIKE` which performs a full table scan on the `part` table — fast for typical usage (<100 sessions), but may lag on very large databases

### Untested Fields

The following fields exist in the tool and schema but could not be verified with real data in this DB — no sessions had them populated:

| Field | DB column | Why missing |
|-------|-----------|-------------|
| Project name (resolved) | `project.name` | All sessions have empty project names in the `project` table |
| Compacted time | `session.time_compacting` | No session has ever been compacted |
| Share URL | `session.share_url` | No session has been shared |
| Diffs summary | `session.summary_diffs` | Not populated for any session |

**Note for future validation:** To test these, a session needs to hit compaction, a share action, or have a non-null project name registered.

---

## 5. Forensic Comparison

Below is a real-world comparison between running **manual SQLite queries** (5+ separate commands) vs a single `opencode-session` call on the same session (`ses_1fcbea243ffeXhW1e9Lo8H7ia3` — a 207-message, ~11.5h build session):

| Data Point | Manual SQL (5+ queries) | `opencode-session` (1 command) |
|---|---|---|
| Session header | ✅ | ✅ |
| Messages / Todos | 207, 6/6 done | 207, 6/6 done |
| **Tool Usage** | | |
| read | 81 | 81 |
| edit | 70 | 70 |
| bash | 67 | 67 |
| write | 23 | 23 |
| serena_write_memory | 12 | 12 |
| serena_edit_memory | 1 | 1 |
| **Part Types** | | |
| tool | 267 | 267 |
| step-start | 170 | 170 |
| step-finish | 170 | 170 |
| reasoning | 145 | 145 |
| text | 52 * | **109** |
| compaction | — | **2** |
| **Finish Reasons** | | |
| stop | 34 | 34 |
| tool-calls | 136 | 136 |
| Message IDs shown | ❌ | ✅ |
| Chronological order | ✅ (ASC) | ✅ with `--all` |
| Commands needed | **5+** | **1** |

*\* Manual query had `AND role = 'assistant'` filter, missing user text parts — the tool's unfiltered count is the correct total.*

**Takeaway:** The tool produces a superset of what raw SQL can extract, with richer formatting, message IDs for turn tracking, and forensic-level breakdowns — all in a single command. Zero SQL knowledge required.
