# opencode-session-viewer

CLI script to inspect [opencode](https://opencode.ai) session metadata from the local SQLite DB.

**Source:** `opencode-session.py`
**Symlink:** `~/.local/bin/opencode-session`

## Quick start

```bash
# link the script
ln -sf "$(pwd)/opencode-session.py" ~/.local/bin/opencode-session

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

A CLI script that queries opencode's local SQLite DB (`~/.local/share/opencode/opencode.db`) to inspect session metadata — title, project, model, message count, todo progress, diff stats, and recent messages with their actual content.

---

## 2. Why

### Comparison with `opencode session list`

The built-in `opencode session list` shows only **3 fields**: Session ID, Title, and last Updated time.

| Data | `opencode session list` | `opencode-session` |
|------|------------------------|-------------------|
| Session ID | ✅ | ✅ |
| Title | ✅ | ✅ |
| Updated time | ✅ | ✅ |
| Slug | ❌ | ✅ |
| Project name / path | ❌ | ✅ |
| Agent used | ❌ | ✅ |
| Model used | ❌ | ✅ |
| Created time | ❌ | ✅ |
| Message count | ❌ | ✅ |
| Todo list with status | ❌ | ✅ |
| Recent messages | ❌ | ✅ |

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

1. **List mode** (no args or `--limit N`): `SELECT` recent N sessions (default 30) ordered by `time_created DESC`, joined with `project` for name resolution.
2. **Detail mode** (session ID arg): Fetches the session row + counts for messages/todos via subqueries. Parses the JSON `model` field to extract the model ID. Formats timestamps (stored as ms epoch) to UTC.
3. **Message rendering**: Fetches last 10 messages, then for each looks up its `part` rows to extract the actual text (user prompts, assistant reasoning, responses, tool calls). Parts are grouped by message and rendered by type — `text` (user/assistant messages), `reasoning` (thinking), `tool` (invoked tool name), `tool-result`, etc.

### Invocation

| Method | Command |
|--------|---------|
| Terminal | `opencode-session [--limit N] [session-id]` |
| opencode TUI (raw output) | `!opencode-session ses_xxx` |
| opencode TUI (LLM-assisted) | `/session ses_xxx` |

### Commands

| Command | Mode | Description |
|---------|------|-------------|
| `opencode-session` | List | Show last 30 sessions |
| `opencode-session --limit 100` | List | Show last N sessions |
| `opencode-session ses_xxx` | Detail | Show full session details, messages, and todos |
| `opencode-session ses_xxx --limit 5` | Detail | `--limit` ignored in detail mode; same as above |
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
| Audit teammate sessions | List sessions by project, check what model/agent was used and what files were changed |
| Debug compaction issues | Check `time_compacting` — if absent, session never compacted; if very recent, might explain context loss |
| Track completion rate | `opencode-session` shows todos_done / total — useful for standup or progress reviews |
| Find a session by project | List all sessions, grep by project name to find the right session ID quickly |
| Verify model usage | Confirm a session used the intended model (e.g., `kimi-k2.5` vs `big-pickle`) — useful when testing model configs |
| Pre-populate a new prompt | `!opencode-session ses_xxx` inside the TUI gives the LLM full context of prior work without manual copy-paste |

### Limitations

- Read-only — never writes to the DB
- Depends on `python3` (stdlib only: `sqlite3`, `json`, `datetime` — no pip packages needed)
- Model field parsing is best-effort (some sessions store raw string, others JSON)

### Untested Fields

The following fields exist in the script and schema but could not be verified with real data in this DB — no sessions had them populated:

| Field | DB column | Why missing |
|-------|-----------|-------------|
| Project name (resolved) | `project.name` | All 64 sessions have empty project names in the `project` table |
| Compacted time | `session.time_compacting` | No session has ever been compacted |
| Share URL | `session.share_url` | No session has been shared |
| Diffs summary | `session.summary_diffs` | Not populated for any session |

**Note for future validation:** To test these, a session needs to hit compaction, a share action, or have a non-null project name registered.
