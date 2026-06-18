package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	_ "modernc.org/sqlite"
)

const defaultLimit = 30

var dbPath string

func init() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: cannot find home dir:", err)
		os.Exit(1)
	}
	dbPath = filepath.Join(home, ".local", "share", "opencode", "opencode.db")
}

func fmtTS(ms int64) string {
	if ms == 0 {
		return "N/A"
	}
	return time.UnixMilli(ms).UTC().Format("2006-01-02 15:04:05")
}

func parseModel(raw sql.NullString) string {
	if !raw.Valid || raw.String == "" {
		return "-"
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(raw.String), &m); err == nil {
		if id, ok := m["id"].(string); ok && id != "" {
			return id
		}
	}
	return raw.String
}

type sessionInfo struct {
	ID      string
	Agent   string
	Title   string
	Created string
	Model   string
}

func scanSessionInfo(rows *sql.Rows) ([]sessionInfo, error) {
	var out []sessionInfo
	for rows.Next() {
		var s sessionInfo
		var agent, model sql.NullString
		if err := rows.Scan(&s.ID, &agent, &s.Title, &s.Created, &model); err != nil {
			return nil, err
		}
		if agent.Valid {
			s.Agent = agent.String
		} else {
			s.Agent = "-"
		}
		s.Model = parseModel(model)
		out = append(out, s)
	}
	return out, rows.Err()
}

func buildTable() table.Writer {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetStyle(table.StyleRounded)
	t.Style().Options.SeparateRows = false
	t.AppendHeader(table.Row{"SESSION", "TITLE", "AGENT", "MODEL", "CREATED"})
	t.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1, WidthMax: 33, WidthMin: 33, Align: text.AlignLeft, AlignHeader: text.AlignLeft},
		{Number: 2, WidthMax: 50, WidthMin: 15, Align: text.AlignLeft, AlignHeader: text.AlignLeft},
		{Number: 3, WidthMax: 10, WidthMin: 10, Align: text.AlignLeft, AlignHeader: text.AlignLeft},
		{Number: 4, WidthMax: 22, WidthMin: 22, Align: text.AlignLeft, AlignHeader: text.AlignLeft},
		{Number: 5, WidthMax: 22, WidthMin: 22, Align: text.AlignLeft, AlignHeader: text.AlignLeft},
	})
	return t
}

func listSessions(db *sql.DB, limit int) {
	rows, err := db.Query(`
		SELECT s.id, s.agent, s.title,
		       datetime(s.time_created/1000, 'unixepoch') as created,
		       s.model
		FROM session s
		WHERE s.parent_id IS NULL
		ORDER BY s.time_created DESC
		LIMIT ?
	`, limit)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error querying sessions:", err)
		os.Exit(1)
	}
	defer rows.Close()

	parents, err := scanSessionInfo(rows)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error scanning parents:", err)
		os.Exit(1)
	}
	if len(parents) == 0 {
		return
	}

	parentIDs := make([]string, len(parents))
	args := make([]interface{}, len(parents))
	for i, p := range parents {
		parentIDs[i] = "?"
		args[i] = p.ID
	}
	placeholders := strings.Join(parentIDs, ",")

	childRows, err := db.Query(fmt.Sprintf(`
		SELECT s.id, s.agent, s.title,
		       datetime(s.time_created/1000, 'unixepoch') as created,
		       s.model,
		       s.parent_id
		FROM session s
		WHERE s.parent_id IN (%s)
		ORDER BY s.time_created ASC
	`, placeholders), args...)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error querying children:", err)
		os.Exit(1)
	}
	defer childRows.Close()

	childrenByParent := map[string][]sessionInfo{}
	for childRows.Next() {
		var s sessionInfo
		var agent, model sql.NullString
		var parentID string
		if err := childRows.Scan(&s.ID, &agent, &s.Title, &s.Created, &model, &parentID); err != nil {
			fmt.Fprintln(os.Stderr, "error scanning child:", err)
			os.Exit(1)
		}
		if agent.Valid {
			s.Agent = agent.String
		} else {
			s.Agent = "-"
		}
		s.Model = parseModel(model)
		childrenByParent[parentID] = append(childrenByParent[parentID], s)
	}

	t := buildTable()
	for _, p := range parents {
		t.AppendRow(table.Row{p.ID, p.Title, p.Agent, p.Model, p.Created})
		for _, c := range childrenByParent[p.ID] {
			t.AppendRow(table.Row{"  " + c.ID, c.Title, c.Agent, c.Model, c.Created})
		}
	}
	t.Render()
}

func searchSessions(db *sql.DB, term string) {
	seen := map[string]sessionInfo{}

	titleRows, err := db.Query(`
		SELECT s.id, s.agent, s.title,
		       datetime(s.time_created/1000, 'unixepoch') as created,
		       s.model
		FROM session s
		WHERE s.title LIKE '%' || ? || '%'
		ORDER BY s.time_created DESC
		LIMIT 30
	`, term)
	if err == nil {
		results, _ := scanSessionInfo(titleRows)
		titleRows.Close()
		for _, s := range results {
			seen[s.ID] = s
		}
	}

	contentRows, err := db.Query(`
		SELECT DISTINCT s.id, s.agent, s.title,
		       datetime(s.time_created/1000, 'unixepoch') as created,
		       s.model
		FROM session s
		JOIN message m ON m.session_id = s.id
		JOIN part p ON p.message_id = m.id
		WHERE json_extract(p.data, '$.type') = 'text'
		  AND json_extract(p.data, '$.text') LIKE '%' || ? || '%'
		ORDER BY s.time_created DESC
		LIMIT 30
	`, term)
	if err == nil {
		results, _ := scanSessionInfo(contentRows)
		contentRows.Close()
		for _, s := range results {
			if _, ok := seen[s.ID]; !ok {
				seen[s.ID] = s
			}
		}
	}

	if len(seen) == 0 {
		fmt.Printf("No sessions found matching: %s\n", term)
		return
	}

	list := make([]sessionInfo, 0, len(seen))
	for _, s := range seen {
		list = append(list, s)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Created > list[j].Created
	})

	fmt.Printf("── Search Results for %q ──\n", term)
	t := buildTable()
	for _, s := range list {
		t.AppendRow(table.Row{s.ID, s.Title, s.Agent, s.Model, s.Created})
	}
	t.Render()
}

func showSession(db *sql.DB, sid string, showAll bool) {
	row := db.QueryRow(`
		SELECT s.id, s.slug, s.title, s.model, s.agent, s.parent_id,
		       s.time_created, s.time_updated, s.time_compacting,
		       s.share_url,
		       s.summary_additions, s.summary_deletions, s.summary_files, s.summary_diffs,
		       coalesce(p.name, s.project_id) as project_name,
		       (SELECT COUNT(*) FROM message WHERE session_id = s.id) as message_count,
		       (SELECT COUNT(*) FROM todo WHERE session_id = s.id) as todo_count,
		       (SELECT COUNT(*) FROM todo WHERE session_id = s.id AND status = 'completed') as todos_done
		FROM session s
		LEFT JOIN project p ON p.id = s.project_id
		WHERE s.id = ?
	`, sid)

	var (
		id, slug, title, projectName string
		modelRaw, shareURL           sql.NullString
		agent                        sql.NullString
		parentID                     sql.NullString
		timeCreated, timeUpdated     sql.NullInt64
		timeCompacting               sql.NullInt64
		summaryAdditions             sql.NullInt64
		summaryDeletions             sql.NullInt64
		summaryFiles                 sql.NullInt64
		summaryDiffs                 sql.NullString
		msgCount, todoCount, todosDone int
	)
	err := row.Scan(
		&id, &slug, &title, &modelRaw, &agent, &parentID,
		&timeCreated, &timeUpdated, &timeCompacting,
		&shareURL,
		&summaryAdditions, &summaryDeletions, &summaryFiles, &summaryDiffs,
		&projectName, &msgCount, &todoCount, &todosDone,
	)
	if err == sql.ErrNoRows {
		fmt.Fprintf(os.Stderr, "Session not found: %s\n", sid)
		os.Exit(1)
	} else if err != nil {
		fmt.Fprintln(os.Stderr, "error querying session:", err)
		os.Exit(1)
	}

	model := parseModel(modelRaw)
	compacted := "N/A"
	if timeCompacting.Valid {
		compacted = fmtTS(timeCompacting.Int64)
	}

	fmt.Printf("Session:       %s\n", id)
	fmt.Printf("Slug:          %s\n", slug)
	fmt.Printf("Title:         %s\n", title)
	fmt.Printf("Project:       %s\n", projectName)
	agt := "-"
	if agent.Valid {
		agt = agent.String
	}
	fmt.Printf("Agent:         %s\n", agt)
	fmt.Printf("Model:         %s\n", model)
	fmt.Printf("Created:       %s\n", fmtTS(timeCreated.Int64))
	fmt.Printf("Updated:       %s\n", fmtTS(timeUpdated.Int64))
	fmt.Println()
	fmt.Println("Stats:")
	fmt.Printf("  Messages:    %d\n", msgCount)
	fmt.Printf("  Todos:       %d (%d done)\n", todoCount, todosDone)
	if summaryAdditions.Valid {
		fmt.Printf("  Additions:   %d\n", summaryAdditions.Int64)
	}
	if summaryDeletions.Valid {
		fmt.Printf("  Deletions:   %d\n", summaryDeletions.Int64)
	}
	if summaryFiles.Valid {
		fmt.Printf("  Files:       %d\n", summaryFiles.Int64)
	}
	if compacted != "N/A" {
		fmt.Printf("  Compacted:   %s\n", compacted)
	}
	if shareURL.Valid && shareURL.String != "" {
		fmt.Printf("  Share URL:   %s\n", shareURL.String)
	}
	if summaryDiffs.Valid && summaryDiffs.String != "" {
		fmt.Printf("  Diffs:       %s\n", summaryDiffs.String)
	}
	fmt.Println()

	if parentID.Valid {
		var pid, pslug, ptitle, pagent string
		db.QueryRow("SELECT id, slug, agent, title FROM session WHERE id = ?",
			parentID.String).Scan(&pid, &pslug, &pagent, &ptitle)
		if pid != "" {
			fmt.Printf("Parent:        %s (%s, %s)\n", pid, pslug, pagent)
			fmt.Println()
		}
	} else {
		crows, err := db.Query(`
			SELECT id, slug, agent, title,
			       datetime(time_created/1000, 'unixepoch') as created
			FROM session
			WHERE parent_id = ?
			ORDER BY time_created ASC
		`, sid)
		if err == nil {
			var children []string
			for crows.Next() {
				var cid, cslug, ctitle, ccreated string
				var cagent sql.NullString
				crows.Scan(&cid, &cslug, &cagent, &ctitle, &ccreated)
				agt := "-"
				if cagent.Valid {
					agt = cagent.String
				}
				children = append(children, fmt.Sprintf("  %s  %-20s %-10s %s", cid, cslug, agt, ccreated))
			}
			crows.Close()
			if len(children) > 0 {
				fmt.Println("Subagents:")
				for _, l := range children {
					fmt.Println(l)
				}
				fmt.Println()
			}
		}
	}

	fmt.Println("── Messages ──")
	order := "DESC"
	limit := "LIMIT 10"
	if showAll {
		order = "ASC"
		limit = ""
	}
	mrows, err := db.Query(fmt.Sprintf(`
		SELECT id, data FROM message
		WHERE session_id = ?
		ORDER BY time_created %s %s
	`, order, limit), sid)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error querying messages:", err)
		os.Exit(1)
	}
	defer mrows.Close()

	hasMsg := false
	for mrows.Next() {
		hasMsg = true
		var mid string
		var dataStr string
		mrows.Scan(&mid, &dataStr)

		var msg map[string]interface{}
		json.Unmarshal([]byte(dataStr), &msg)

		role, _ := msg["role"].(string)

		prows, err := db.Query("SELECT data FROM part WHERE message_id = ? ORDER BY time_created", mid)
		if err != nil {
			continue
		}

		var partsData []map[string]interface{}
		for prows.Next() {
			var pdata string
			prows.Scan(&pdata)
			var pm map[string]interface{}
			json.Unmarshal([]byte(pdata), &pm)
			partsData = append(partsData, pm)
		}
		prows.Close()

		midShort := mid
		if len(mid) > 8 {
			midShort = mid[len(mid)-8:]
		}

		if role == "user" {
			text := ""
			for _, p := range partsData {
				if p["type"] == "text" {
					if t, ok := p["text"].(string); ok {
						text = t
						break
					}
				}
			}
			preview := "(no text)"
			if text != "" {
				preview = strings.ReplaceAll(text, "\n", " ")
				if len(preview) > 90 {
					preview = preview[:90]
				}
			}
			fmt.Printf("  [user:%s] %s\n", midShort, preview)
		} else {
			var info []string
			for _, p := range partsData {
				ptype, _ := p["type"].(string)
				switch ptype {
				case "text":
					t, _ := p["text"].(string)
					t = strings.ReplaceAll(t, "\n", " ")
					if len(t) > 90 {
						t = t[:90]
					}
					if t != "" {
						info = append(info, "text: "+t)
					} else {
						info = append(info, "text")
					}
				case "reasoning":
					t, _ := p["text"].(string)
					t = strings.ReplaceAll(t, "\n", " ")
					if len(t) > 60 {
						t = t[:60]
					}
					if t != "" {
						info = append(info, "reasoning: "+t)
					} else {
						info = append(info, "reasoning")
					}
				case "tool":
					tool, _ := p["tool"].(string)
					info = append(info, "tool:"+tool)
				case "step-start":
					info = append(info, "step-start")
				case "tool-result":
					info = append(info, "tool-result")
				}
			}
			if finish, ok := msg["finish"].(string); ok && finish != "" {
				info = append(info, "finish="+finish)
			}
			fmt.Printf("  [%s:%s] %s\n", role, midShort, strings.Join(info, " | "))
		}
	}
	if !hasMsg {
		fmt.Println("  (none)")
	}

	trows, err := db.Query(
		"SELECT content, status, priority FROM todo WHERE session_id = ? ORDER BY position",
		sid,
	)
	if err == nil {
		var todos []string
		for trows.Next() {
			var content, status, priority string
			trows.Scan(&content, &status, &priority)
			todos = append(todos, fmt.Sprintf("%s │ %s │ %s", content, status, priority))
		}
		trows.Close()
		if len(todos) > 0 {
			fmt.Println()
			fmt.Println("── Todos ──")
			for _, t := range todos {
				fmt.Println(t)
			}
		}
	}

	fmt.Println()
	forensicStats(db, sid)
}

func forensicStats(db *sql.DB, sid string) {
	fmt.Println("── Forensic Stats ──")

	toolRows, err := db.Query(`
		SELECT json_extract(p.data, '$.tool') as tool_name, COUNT(*) as cnt
		FROM part p
		JOIN message m ON m.id = p.message_id
		WHERE m.session_id = ? AND json_extract(p.data, '$.type') = 'tool'
		GROUP BY 1 ORDER BY cnt DESC
	`, sid)
	if err == nil {
		var tools []string
		for toolRows.Next() {
			var name string
			var cnt int
			toolRows.Scan(&name, &cnt)
			tools = append(tools, fmt.Sprintf("    %-22s %d", name, cnt))
		}
		toolRows.Close()
		if len(tools) > 0 {
			fmt.Println("  Tool Usage:")
			for _, l := range tools {
				fmt.Println(l)
			}
			fmt.Println()
		}
	}

	partRows, err := db.Query(`
		SELECT json_extract(p.data, '$.type') as ptype, COUNT(*) as cnt
		FROM part p
		JOIN message m ON m.id = p.message_id
		WHERE m.session_id = ?
		GROUP BY 1 ORDER BY cnt DESC
	`, sid)
	if err == nil {
		var parts []string
		for partRows.Next() {
			var ptype string
			var cnt int
			partRows.Scan(&ptype, &cnt)
			parts = append(parts, fmt.Sprintf("    %-22s %d", ptype, cnt))
		}
		partRows.Close()
		if len(parts) > 0 {
			fmt.Println("  Part Types:")
			for _, l := range parts {
				fmt.Println(l)
			}
			fmt.Println()
		}
	}

	finishRows, err := db.Query(`
		SELECT json_extract(m.data, '$.finish') as finish, COUNT(*) as cnt
		FROM message m
		WHERE m.session_id = ? AND json_extract(m.data, '$.role') = 'assistant'
		GROUP BY 1
	`, sid)
	if err == nil {
		var finishes []string
		for finishRows.Next() {
			var finish sql.NullString
			var cnt int
			finishRows.Scan(&finish, &cnt)
			f := "none"
			if finish.Valid && finish.String != "" {
				f = finish.String
			}
			finishes = append(finishes, fmt.Sprintf("    %-22s %d", f, cnt))
		}
		finishRows.Close()
		if len(finishes) > 0 {
			fmt.Println("  Finish Reasons:")
			for _, l := range finishes {
				fmt.Println(l)
			}
			fmt.Println()
		}
	}
}

func main() {
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: opencode database not found at %s\n", dbPath)
		os.Exit(1)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error opening database:", err)
		os.Exit(1)
	}
	defer db.Close()

	if len(os.Args) < 2 || os.Args[1][0] != '-' {
		if len(os.Args) >= 2 && os.Args[1][0] != '-' {
			showSession(db, os.Args[1], false)
			return
		}
		listSessions(db, defaultLimit)
		return
	}

	var limit = defaultLimit
	var showAll bool
	var searchTerm string
	var sid string

	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--limit", "-n":
			if i+1 < len(args) {
				fmt.Sscanf(args[i+1], "%d", &limit)
				i++
			}
		case "--all", "-a":
			showAll = true
		case "--search", "-s":
			if i+1 < len(args) {
				searchTerm = args[i+1]
				i++
			} else {
				fmt.Fprintln(os.Stderr, "Error: --search requires a term")
				os.Exit(1)
			}
		default:
			if !strings.HasPrefix(args[i], "-") {
				sid = args[i]
			} else {
				fmt.Fprintf(os.Stderr, "Unknown option: %s\n", args[i])
				fmt.Fprintln(os.Stderr, "Usage: opencode-session [--limit N] [--all] [--search TERM|-s TERM] [session-id]")
				os.Exit(1)
			}
		}
	}

	switch {
	case searchTerm != "":
		searchSessions(db, searchTerm)
	case sid != "":
		showSession(db, sid, showAll)
	default:
		listSessions(db, limit)
	}
}
