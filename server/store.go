package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
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
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Upsert inserts a finding or refreshes its updated_at if the same
// (project, gate_id, file_path) tuple already exists. Resolved/ignored
// findings get reopened when re-detected.
func (s *Store) Upsert(ctx context.Context, f Finding) (*Finding, error) {
	now := time.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO findings (project, gate_id, severity, title, message, file_path, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 'open', ?, ?)
		ON CONFLICT(project, gate_id, file_path) DO UPDATE SET
			severity = excluded.severity,
			title    = excluded.title,
			message  = excluded.message,
			status   = CASE WHEN findings.status = 'ignored' THEN 'ignored' ELSE 'open' END,
			updated_at = excluded.updated_at
	`, f.Project, f.GateID, f.Severity, f.Title, f.Message, f.FilePath, now, now)
	if err != nil {
		return nil, err
	}
	return s.GetByKey(ctx, f.Project, f.GateID, f.FilePath)
}

func (s *Store) GetByKey(ctx context.Context, project, gateID, filePath string) (*Finding, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, project, gate_id, severity, title, message, file_path, status, created_at, updated_at
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
	err := s.Scan(&f.ID, &f.Project, &f.GateID, &f.Severity, &f.Title, &f.Message, &f.FilePath, &f.Status, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &f, nil
}

// List returns findings for a project (or all projects if project == "").
// Filters by status; pass "" to include any status.
func (s *Store) List(ctx context.Context, project, status string, limit int) ([]Finding, error) {
	if limit <= 0 {
		limit = 200
	}
	q := `SELECT id, project, gate_id, severity, title, message, file_path, status, created_at, updated_at FROM findings WHERE 1=1`
	args := []any{}
	if project != "" {
		q += ` AND project = ?`
		args = append(args, project)
	}
	if status != "" {
		q += ` AND status = ?`
		args = append(args, status)
	}
	q += ` ORDER BY updated_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Finding{}
	for rows.Next() {
		f, err := scanFinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
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
