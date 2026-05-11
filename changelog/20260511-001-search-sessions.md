# Search Sessions by Title or Message Content

**Ticket:** 001  
**Date:** 2026-05-11  
**Status:** Completed

---

## 1. Goal

Add a `--search` / `-s` flag to `opencode-session-viewer` that lets users search for sessions by:
- Session **title** (substring match)
- Message **content** (text inside conversation parts)

Both searches run simultaneously; results are deduplicated by session ID.

---

## 2. Interface

```
opencode-session --search "some term"
opencode-session -s "some term"
```

Output format (list of matching sessions):

```
── Search Results for "some term" ──
ses_abc123 │ 2026-05-10 │ Title matches: "Fix login bug"
ses_xyz789 │ 2026-05-09 │ Content matches: "the error was in auth handler"
```

---

## 3. Implementation

### 3.1 Flag parsing

Add `--search` / `-s` to `args` parsing block. Store term in a `search_term` variable.

### 3.2 Search function: `search_sessions(term)`

Runs two SQL queries and unions results:

**Title search:**
```sql
SELECT s.id, s.title, datetime(s.time_created/1000, 'unixepoch') as created, 'title' as match_type
FROM session s
WHERE s.title LIKE '%' || ? || '%'
ORDER BY s.time_created DESC
LIMIT 30
```

**Content search (message parts):**
```sql
SELECT DISTINCT s.id, s.title, datetime(s.time_created/1000, 'unixepoch') as created, 'content' as match_type
FROM session s
JOIN message m ON m.session_id = s.id
JOIN part p ON p.message_id = m.id
WHERE json_extract(p.data, '$.type') = 'text'
  AND json_extract(p.data, '$.text') LIKE '%' || ? || '%'
ORDER BY s.time_created DESC
LIMIT 30
```

### 3.3 Deduplication

Use a dict keyed by session ID. If a session matches both title and content, prefer showing the title match reason.

### 3.4 Output

```
── Search Results for "<term>" ──
<id> │ <created> │ <match_label>: "<truncated_context>"
```

Where `match_label` is `"Title matches"` or `"Content matches"`, and context shows ~50 chars around the match position with `...` truncation markers.

---

## 4. Scope

- **In scope:** `--search` / `-s` flag, case-insensitive LIKE search, deduplication, output formatting
- **Out of scope:** Regex search, FTS (Full Text Search), pagination, search-by-date

---

## 5. Risks

- SQLite `LIKE` is case-insensitive for ASCII by default — fine for this use case
- Message part JSON extraction could be slow on very large DBs — add LIMIT to protect

---

## 6. Post-Review Fixes

| Issue | Fix |
|-------|-----|
| Content preview showed first 60 bytes, missing the match term | Now uses `instr()` to find match position and shows ~50 chars around it with `...` markers |
| Missing perf note on JSON_EXTRACT | Added to README Limitations section |

---

## 7. Test Results

**Branch:** `feature/001-search-sessions`
**DB:** Local opencode SQLite database

| # | Scenario | Command | Status |
|---|----------|---------|--------|
| 1 | Title match | `--search "review"` | ✅ Pass |
| 2 | Content match | `-s "error"` | ✅ Pass |
| 3 | Both match (dedup) | `-s "login"` | ✅ Pass |
| 4 | No results | `-s "zzzzznotexist999"` | ✅ Pass — clean message |
| 5 | Empty term | `--search ""` | ✅ Pass — error |
| 6 | Missing value | `--search` alone | ✅ Pass — error |
| 7 | Special chars | `-s "auth handler"` | ✅ Pass |
| 8 | List regression | `opencode-session` | ✅ Pass |
| 9 | Detail regression | `ses_xxx` | ✅ Pass |
| 10 | `--all` regression | `ses_xxx --all` | ✅ Pass |
| 11 | `--limit` regression | `-n 5` | ✅ Pass |

**11/11 passed.** All existing functionality preserved, context-aware content preview working correctly.
