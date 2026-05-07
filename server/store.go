package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Finding is a single gate violation observed in a project.
type Finding struct {
	ID        int64  `json:"id"`
	Project   string `json:"project"`
	GateID    string `json:"gate_id"`
	Severity  string `json:"severity"`
	Title     string `json:"title"`
	Message   string `json:"message"`
	FilePath  string `json:"file_path"`
	Tags      string `json:"tags"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

const (
	StatusOpen     = "open"
	StatusResolved = "resolved"
	StatusIgnored  = "ignored"

	SeverityInfo    = "info"
	SeverityWarning = "warning"
	SeverityError   = "error"
)

type Store struct {
	db *sql.DB
}

var ErrNotFound = errors.New("finding not found")

func defaultDBPath() (string, error) {
	if p := os.Getenv("LGIT_DB"); p != "" {
		if dir := filepath.Dir(p); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return "", err
			}
		}
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".l0-git")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "findings.db"), nil
}

func OpenStore() (*Store, error) {
	path, err := defaultDBPath()
	if err != nil {
		return nil, err
	}
	return openStoreAt(path)
}

func openStoreAt(path string) (*Store, error) {
	// busy_timeout(15000): the extension may spawn `lgit check` concurrently
	// with `lgit list` (tree refresh), and Claude Code can hold an MCP-mode
	// process at the same time. 15 s is enough for cross-process WAL recovery
	// without making genuinely-stuck calls block the UI for too long.
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(15000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	schema := `
	CREATE TABLE IF NOT EXISTS findings (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		project     TEXT NOT NULL,
		gate_id     TEXT NOT NULL,
		severity    TEXT NOT NULL,
		title       TEXT NOT NULL,
		message     TEXT NOT NULL,
		file_path   TEXT NOT NULL DEFAULT '',
		tags        TEXT NOT NULL DEFAULT '',
		status      TEXT NOT NULL DEFAULT 'open',
		created_at  INTEGER NOT NULL,
		updated_at  INTEGER NOT NULL,
		UNIQUE(project, gate_id, file_path)
	);
	CREATE INDEX IF NOT EXISTS idx_findings_project ON findings(project, status);
	CREATE INDEX IF NOT EXISTS idx_findings_updated ON findings(updated_at DESC);
	`
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, err
	}
	// Migrations for DBs created before a column was added. SQLite has no
	// "ADD COLUMN IF NOT EXISTS"; the cheapest reliable check is to try
	// the ALTER and tolerate the "duplicate column" failure.
	if err := addColumnIfMissing(db, "findings", "tags", "TEXT NOT NULL DEFAULT ''"); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func addColumnIfMissing(db *sql.DB, table, column, decl string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	_, err = db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + decl)
	return err
}

func (s *Store) Close() error { return s.db.Close() }

// Upsert inserts a finding or refreshes its updated_at if the same
// (project, gate_id, file_path) tuple already exists. Resolved/ignored
// findings get reopened when re-detected.
func (s *Store) Upsert(ctx context.Context, f Finding) (*Finding, error) {
	now := time.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO findings (project, gate_id, severity, title, message, file_path, tags, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'open', ?, ?)
		ON CONFLICT(project, gate_id, file_path) DO UPDATE SET
			severity = excluded.severity,
			title    = excluded.title,
			message  = excluded.message,
			tags     = excluded.tags,
			status   = CASE WHEN findings.status = 'ignored' THEN 'ignored' ELSE 'open' END,
			updated_at = excluded.updated_at
	`, f.Project, f.GateID, f.Severity, f.Title, f.Message, f.FilePath, f.Tags, now, now)
	if err != nil {
		return nil, err
	}
	return s.GetByKey(ctx, f.Project, f.GateID, f.FilePath)
}

func (s *Store) GetByKey(ctx context.Context, project, gateID, filePath string) (*Finding, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, project, gate_id, severity, title, message, file_path, tags, status, created_at, updated_at
		FROM findings
		WHERE project = ? AND gate_id = ? AND file_path = ?
	`, project, gateID, filePath)
	return scanFinding(row)
}

type scannable interface {
	Scan(dest ...any) error
}

func scanFinding(s scannable) (*Finding, error) {
	var f Finding
	err := s.Scan(&f.ID, &f.Project, &f.GateID, &f.Severity, &f.Title, &f.Message, &f.FilePath, &f.Tags, &f.Status, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &f, nil
}

// FindingFilter is the structured query used by the rich-list endpoint.
// Empty string fields mean "no filter on this dimension"; Limit==0 falls
// back to a sensible default (500). Sort accepts a small whitelist —
// unknown values fall back to updated_at DESC.
type FindingFilter struct {
	Project  string
	Status   string
	Severity string
	GateID   string
	Tag      string
	Query    string
	Sort     string
	Offset   int
	Limit    int
}

// sortOrderings is the sort whitelist. Keys are the user-facing identifiers;
// values are SQL fragments inserted directly into ORDER BY (so they MUST be
// hand-vetted, never user input).
var sortOrderings = map[string]string{
	"updated":  "updated_at DESC, id DESC",
	"created":  "created_at DESC, id DESC",
	"severity": "CASE severity WHEN 'error' THEN 0 WHEN 'warning' THEN 1 ELSE 2 END, updated_at DESC",
	"gate":     "gate_id ASC, updated_at DESC",
	"file":     "file_path ASC, updated_at DESC",
	"":         "updated_at DESC, id DESC",
}

// List runs the filtered query. Each filter dimension is optional. Tag
// matching is CSV-aware: a stored value of "security,git-hygiene" is
// considered to contain "security" and "git-hygiene" but not "git".
func (s *Store) List(ctx context.Context, f FindingFilter) ([]Finding, error) {
	if f.Limit <= 0 {
		f.Limit = 500
	}
	q := `SELECT id, project, gate_id, severity, title, message, file_path, tags, status, created_at, updated_at FROM findings WHERE 1=1`
	args := []any{}
	if f.Project != "" {
		q += ` AND project = ?`
		args = append(args, f.Project)
	}
	if f.Status != "" {
		q += ` AND status = ?`
		args = append(args, f.Status)
	}
	if f.Severity != "" {
		q += ` AND severity = ?`
		args = append(args, f.Severity)
	}
	if f.GateID != "" {
		q += ` AND gate_id = ?`
		args = append(args, f.GateID)
	}
	if f.Tag != "" {
		// (',' || tags || ',') LIKE '%,<tag>,%' — matches whole CSV
		// elements only, so "git" doesn't accidentally match "git-hygiene".
		q += ` AND (',' || tags || ',') LIKE ?`
		args = append(args, "%,"+f.Tag+",%")
	}
	if f.Query != "" {
		q += ` AND (title LIKE ? OR message LIKE ? OR file_path LIKE ? OR gate_id LIKE ?)`
		like := "%" + f.Query + "%"
		args = append(args, like, like, like, like)
	}
	order, ok := sortOrderings[f.Sort]
	if !ok {
		order = sortOrderings[""]
	}
	q += ` ORDER BY ` + order + ` LIMIT ? OFFSET ?`
	args = append(args, f.Limit, f.Offset)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Finding{}
	for rows.Next() {
		fnd, err := scanFinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *fnd)
	}
	return out, rows.Err()
}

// MarkResolved closes any open findings for (project, gate_id) whose file_path
// is NOT in keep. Returns the number of rows updated. Used after a fresh check
// to retire findings that the gate no longer reports.
func (s *Store) MarkResolved(ctx context.Context, project, gateID string, keep []string) (int, error) {
	now := time.Now().UnixMilli()
	if len(keep) == 0 {
		res, err := s.db.ExecContext(ctx, `
			UPDATE findings SET status = 'resolved', updated_at = ?
			WHERE project = ? AND gate_id = ? AND status = 'open'
		`, now, project, gateID)
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		return int(n), nil
	}
	// Build a parameterized NOT IN clause.
	q := `UPDATE findings SET status = 'resolved', updated_at = ?
		WHERE project = ? AND gate_id = ? AND status = 'open' AND file_path NOT IN (`
	args := []any{now, project, gateID}
	for i, k := range keep {
		if i > 0 {
			q += ","
		}
		q += "?"
		args = append(args, k)
	}
	q += ")"
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *Store) Delete(ctx context.Context, id int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM findings WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Ignore marks a finding as ignored so future re-runs of the gate don't
// resurface it for the same (project, gate_id, file_path).
func (s *Store) Ignore(ctx context.Context, id int64) (bool, error) {
	now := time.Now().UnixMilli()
	res, err := s.db.ExecContext(ctx, `UPDATE findings SET status = 'ignored', updated_at = ? WHERE id = ?`, now, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) ClearProject(ctx context.Context, project string) (int, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM findings WHERE project = ?`, project)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// FindingsStats is the aggregate the dashboard renders. Severity/status
// counts cover ALL statuses so users see the full picture; gate/file/tag
// breakdowns and the 7-day trend are scoped to currently-open findings —
// they're the actionable set.
type FindingsStats struct {
	Project    string         `json:"project"`
	Total      int            `json:"total"`
	BySeverity map[string]int `json:"by_severity"`
	ByStatus   map[string]int `json:"by_status"`
	ByGate     []KeyCount     `json:"by_gate"`
	ByTag      []KeyCount     `json:"by_tag"`
	TopFiles   []KeyCount     `json:"top_files"`
	Last7Days  []DayCount     `json:"last_7_days"`
}

// KeyCount is a generic "label → count" row used for ranked breakdowns.
type KeyCount struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

// DayCount carries one day-bucket of the trend chart. Date is YYYY-MM-DD
// in UTC so the dashboard can render it without further parsing.
type DayCount struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// Stats computes every aggregation the Overview webview needs in one trip.
// Empty project means "across all projects" — useful for a global view.
func (s *Store) Stats(ctx context.Context, project string) (*FindingsStats, error) {
	out := &FindingsStats{
		Project:    project,
		BySeverity: map[string]int{},
		ByStatus:   map[string]int{},
		ByGate:     []KeyCount{},
		ByTag:      []KeyCount{},
		TopFiles:   []KeyCount{},
		Last7Days:  []DayCount{},
	}

	whereProject, projectArgs := projectClause(project)

	// Status spans every row (so users see how many were resolved/ignored).
	// Total derives from it.
	if err := s.scanCount(ctx, &out.ByStatus,
		`SELECT status, COUNT(*) FROM findings`+whereProject+` GROUP BY status`, projectArgs); err != nil {
		return nil, err
	}
	for _, n := range out.ByStatus {
		out.Total += n
	}

	// Severity / gate / file / tag breakdowns are all open-only — they
	// describe what the user has to act on right now, not what's been
	// dealt with. Mixing statuses here was confusing in practice.
	openClause := whereProject + appendCondition(whereProject, "status = 'open'")

	if err := s.scanCount(ctx, &out.BySeverity,
		`SELECT severity, COUNT(*) FROM findings`+openClause+` GROUP BY severity`, projectArgs); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT gate_id, COUNT(*) AS n FROM findings`+openClause+
			` GROUP BY gate_id ORDER BY n DESC, gate_id ASC LIMIT 50`, projectArgs...)
	if err != nil {
		return nil, err
	}
	out.ByGate, err = scanKeyCount(rows)
	if err != nil {
		return nil, err
	}

	// Top files: strip the `:line:rule_id` suffix that scan-style gates
	// stamp onto file_path; group by the leading file path.
	rows, err = s.db.QueryContext(ctx,
		`SELECT
			CASE WHEN INSTR(file_path, ':') > 0
				THEN SUBSTR(file_path, 1, INSTR(file_path, ':') - 1)
				ELSE file_path END AS stem,
			COUNT(*) AS n
		FROM findings`+openClause+` AND file_path != ''
		GROUP BY stem ORDER BY n DESC, stem ASC LIMIT 10`, projectArgs...)
	if err != nil {
		return nil, err
	}
	out.TopFiles, err = scanKeyCount(rows)
	if err != nil {
		return nil, err
	}

	// Tags: explode CSV in Go since SQLite has no native split.
	tagRows, err := s.db.QueryContext(ctx,
		`SELECT tags FROM findings`+openClause+` AND tags != ''`, projectArgs...)
	if err != nil {
		return nil, err
	}
	out.ByTag = explodeTags(tagRows)

	// 7-day trend on created_at. SQLite fragment 86400000 = ms/day.
	cutoff := time.Now().Add(-7 * 24 * time.Hour).UnixMilli()
	dayRows, err := s.db.QueryContext(ctx,
		`SELECT created_at / 86400000 AS day, COUNT(*) AS n
		 FROM findings`+whereProject+
			appendCondition(whereProject, "created_at >= ?")+
			` GROUP BY day ORDER BY day ASC`,
		append(append([]any{}, projectArgs...), cutoff)...)
	if err != nil {
		return nil, err
	}
	out.Last7Days = build7DayTrend(dayRows, time.Now())

	return out, nil
}

func projectClause(project string) (string, []any) {
	if project == "" {
		return "", nil
	}
	return " WHERE project = ?", []any{project}
}

func appendCondition(existing, cond string) string {
	if existing == "" {
		return " WHERE " + cond
	}
	return " AND " + cond
}

func (s *Store) scanCount(ctx context.Context, into *map[string]int, q string, args []any) error {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return err
		}
		(*into)[k] = n
	}
	return rows.Err()
}

func scanKeyCount(rows *sql.Rows) ([]KeyCount, error) {
	defer rows.Close()
	out := []KeyCount{}
	for rows.Next() {
		var kc KeyCount
		if err := rows.Scan(&kc.Key, &kc.Count); err != nil {
			return nil, err
		}
		out = append(out, kc)
	}
	return out, rows.Err()
}

func explodeTags(rows *sql.Rows) []KeyCount {
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var tags string
		if err := rows.Scan(&tags); err != nil {
			continue
		}
		for _, t := range strings.Split(tags, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				counts[t]++
			}
		}
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		return keys[i] < keys[j]
	})
	out := make([]KeyCount, 0, len(keys))
	for _, k := range keys {
		out = append(out, KeyCount{Key: k, Count: counts[k]})
	}
	return out
}

// build7DayTrend pads the SQL result so callers always get exactly 7
// entries — today plus the prior six days, oldest first. Each entry
// carries a YYYY-MM-DD date string in UTC.
func build7DayTrend(rows *sql.Rows, now time.Time) []DayCount {
	defer rows.Close()
	bucket := map[int64]int{}
	for rows.Next() {
		var day int64
		var n int
		if err := rows.Scan(&day, &n); err != nil {
			continue
		}
		bucket[day] = n
	}
	out := make([]DayCount, 0, 7)
	today := now.UTC().Truncate(24 * time.Hour)
	for i := 6; i >= 0; i-- {
		d := today.Add(time.Duration(-i) * 24 * time.Hour)
		dayIdx := d.UnixMilli() / int64(86400000)
		out = append(out, DayCount{
			Date:  d.Format("2006-01-02"),
			Count: bucket[dayIdx],
		})
	}
	return out
}
