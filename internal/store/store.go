// Package store persists sessions and their immutable event log in SQLite
// (pure-Go driver, WAL mode). Schema and rationale in SPEC.md §7.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	_ "modernc.org/sqlite"

	"xanax/internal/session"
)

var (
	ErrNotFound  = errors.New("session not found")
	ErrAmbiguous = errors.New("session ID prefix is ambiguous")
)

// migrations are forward-only; settings.schema_version records how many have
// been applied. Append new statements, never edit shipped ones.
var migrations = []string{`
CREATE TABLE sessions (
  id                  TEXT PRIMARY KEY,
  title               TEXT NOT NULL,
  repo_path           TEXT NOT NULL,
  branch              TEXT,
  harness             TEXT NOT NULL,
  harness_session_ref TEXT,
  initial_prompt      TEXT,
  status              TEXT NOT NULL,
  status_detail       TEXT,
  pid                 INTEGER,
  socket_path         TEXT,
  exit_code           INTEGER,
  created_at          TEXT NOT NULL,
  updated_at          TEXT NOT NULL,
  ended_at            TEXT
);
CREATE INDEX idx_sessions_status ON sessions(status);

CREATE TABLE events (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL REFERENCES sessions(id),
  ts         TEXT NOT NULL,
  type       TEXT NOT NULL,
  payload    TEXT
);
CREATE INDEX idx_events_session ON events(session_id, id);

CREATE TABLE repositories (
  path         TEXT PRIMARY KEY,
  name         TEXT NOT NULL,
  last_used_at TEXT NOT NULL
);
`}

// Event is one row of the append-only event log.
type Event struct {
	ID        int64     `json:"id"`
	SessionID string    `json:"session_id"`
	TS        time.Time `json:"ts"`
	Type      string    `json:"type"`
	Payload   string    `json:"payload,omitempty"` // raw JSON
}

type Store struct {
	db *sql.DB
}

// Open creates the database (and parent directory) if needed, enables WAL,
// and applies pending migrations. Safe to call from multiple processes.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		return err
	}
	version := 0
	var raw string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = 'schema_version'`).Scan(&raw)
	switch {
	case err == nil:
		version, err = strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("corrupt schema_version %q", raw)
		}
	case errors.Is(err, sql.ErrNoRows):
	default:
		return err
	}
	if version > len(migrations) {
		return fmt.Errorf("database schema version %d is newer than this xanax build (max %d)", version, len(migrations))
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(
			`INSERT INTO settings (key, value) VALUES ('schema_version', ?)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			strconv.Itoa(i+1),
		); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// CreateSession inserts a new session and stamps CreatedAt/UpdatedAt.
func (s *Store) CreateSession(sess *session.Session) error {
	now := time.Now().UTC()
	sess.CreatedAt, sess.UpdatedAt = now, now
	_, err := s.db.Exec(`
		INSERT INTO sessions (
			id, title, repo_path, branch, harness, harness_session_ref,
			initial_prompt, status, status_detail, pid, socket_path,
			exit_code, created_at, updated_at, ended_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.Title, sess.RepoPath, nullStr(sess.Branch), sess.Harness,
		nullStr(sess.HarnessSessionRef), nullStr(sess.InitialPrompt),
		string(sess.Status), nullStr(sess.StatusDetail), nullInt(sess.PID),
		nullStr(sess.SocketPath), nullIntPtr(sess.ExitCode),
		fmtTime(now), fmtTime(now), nullTimePtr(sess.EndedAt),
	)
	return err
}

// GetSession resolves a full session ID or a unique ID prefix (git-style).
func (s *Store) GetSession(idOrPrefix string) (*session.Session, error) {
	if idOrPrefix == "" {
		return nil, ErrNotFound
	}
	rows, err := s.db.Query(
		selectSessions+` WHERE substr(id, 1, length(?)) = ? ORDER BY created_at DESC`,
		idOrPrefix, idOrPrefix,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var matches []*session.Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		if sess.ID == idOrPrefix {
			return sess, nil
		}
		matches = append(matches, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("%w: %q", ErrNotFound, idOrPrefix)
	case 1:
		return matches[0], nil
	default:
		return nil, fmt.Errorf("%w: %q matches %d sessions", ErrAmbiguous, idOrPrefix, len(matches))
	}
}

// ListSessions returns all sessions, newest first.
func (s *Store) ListSessions() ([]*session.Session, error) {
	rows, err := s.db.Query(selectSessions + ` ORDER BY created_at DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*session.Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

// SetStatus updates the session state and bumps updated_at. The full session
// ID is required (no prefix matching on writes).
func (s *Store) SetStatus(id string, status session.Status, detail string) error {
	// Never resurrect a session that has already reached a terminal state: a
	// late state event (e.g. a generic-adapter idle tick racing shutdown) must
	// not overwrite a completed/failed/cancelled row. Terminal rows are left
	// untouched, not treated as an error.
	res, err := s.db.Exec(
		`UPDATE sessions SET status = ?, status_detail = ?, updated_at = ?
		 WHERE id = ? AND status NOT IN (?, ?, ?)`,
		string(status), nullStr(detail), fmtTime(time.Now().UTC()), id,
		string(session.StatusCompleted), string(session.StatusFailed), string(session.StatusCancelled),
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	// No row updated: either the id is unknown, or it is already terminal.
	// Distinguish so a genuinely missing session is still reported.
	var cur string
	switch err := s.db.QueryRow(`SELECT status FROM sessions WHERE id = ?`, id).Scan(&cur); {
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("%w: %q", ErrNotFound, id)
	case err != nil:
		return err
	default:
		return nil // exists but terminal — intentional no-op
	}
}

// SetRuntime records the supervisor's pid and socket path and moves the
// session to the given (typically running) status.
func (s *Store) SetRuntime(id string, pid int, socketPath string, status session.Status) error {
	return s.exec1(id,
		`UPDATE sessions SET pid = ?, socket_path = ?, status = ?, updated_at = ? WHERE id = ?`,
		pid, socketPath, string(status), fmtTime(time.Now().UTC()), id,
	)
}

// SetTitle renames a session. The title is xanax's UI label only; it does not
// touch the harness's own session.
func (s *Store) SetTitle(id, title string) error {
	return s.exec1(id,
		`UPDATE sessions SET title = ?, updated_at = ? WHERE id = ?`,
		title, fmtTime(time.Now().UTC()), id,
	)
}

// SetSessionRef stores the harness-native resume handle.
func (s *Store) SetSessionRef(id, ref string) error {
	return s.exec1(id,
		`UPDATE sessions SET harness_session_ref = ?, updated_at = ? WHERE id = ?`,
		ref, fmtTime(time.Now().UTC()), id,
	)
}

// Finish records a terminal state, exit code, and end timestamp.
func (s *Store) Finish(id string, status session.Status, exitCode int) error {
	now := fmtTime(time.Now().UTC())
	return s.exec1(id,
		`UPDATE sessions SET status = ?, exit_code = ?, updated_at = ?, ended_at = ? WHERE id = ?`,
		string(status), exitCode, now, now, id,
	)
}

// exec1 runs a single-row UPDATE and maps a zero-row result to ErrNotFound.
// id is used only to build the error message.
func (s *Store) exec1(id, query string, args ...any) error {
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	return nil
}

// DeleteSession removes a session and its event log. Used by the dashboard's
// "kill" action to clear a session from the list.
func (s *Store) DeleteSession(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM events WHERE session_id = ?`, id); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE id = ?`, id); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// RecordEvent appends to the immutable event log. payload is JSON-marshaled;
// pass nil for events without data.
func (s *Store) RecordEvent(sessionID, eventType string, payload any) error {
	var data any
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal event payload: %w", err)
		}
		data = string(b)
	}
	_, err := s.db.Exec(
		`INSERT INTO events (session_id, ts, type, payload) VALUES (?, ?, ?, ?)`,
		sessionID, fmtTime(time.Now().UTC()), eventType, data,
	)
	return err
}

// ListEvents returns a session's events in insertion order.
func (s *Store) ListEvents(sessionID string) ([]Event, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, ts, type, payload FROM events WHERE session_id = ? ORDER BY id`,
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		var ts string
		var payload sql.NullString
		if err := rows.Scan(&e.ID, &e.SessionID, &ts, &e.Type, &payload); err != nil {
			return nil, err
		}
		if e.TS, err = parseTime(ts); err != nil {
			return nil, err
		}
		e.Payload = payload.String
		events = append(events, e)
	}
	return events, rows.Err()
}

// TouchRepository upserts a repository row and bumps last_used_at.
func (s *Store) TouchRepository(path, name string) error {
	_, err := s.db.Exec(
		`INSERT INTO repositories (path, name, last_used_at) VALUES (?, ?, ?)
		 ON CONFLICT(path) DO UPDATE SET name = excluded.name, last_used_at = excluded.last_used_at`,
		path, name, fmtTime(time.Now().UTC()),
	)
	return err
}

const selectSessions = `
	SELECT id, title, repo_path, branch, harness, harness_session_ref,
	       initial_prompt, status, status_detail, pid, socket_path,
	       exit_code, created_at, updated_at, ended_at
	FROM sessions`

func scanSession(rows *sql.Rows) (*session.Session, error) {
	var (
		sess                                                    session.Session
		branch, ref, prompt, detail, socket, createdAt, updated sql.NullString
		endedAt                                                 sql.NullString
		pid, exitCode                                           sql.NullInt64
		status                                                  string
	)
	if err := rows.Scan(
		&sess.ID, &sess.Title, &sess.RepoPath, &branch, &sess.Harness, &ref,
		&prompt, &status, &detail, &pid, &socket, &exitCode,
		&createdAt, &updated, &endedAt,
	); err != nil {
		return nil, err
	}
	sess.Branch = branch.String
	sess.HarnessSessionRef = ref.String
	sess.InitialPrompt = prompt.String
	sess.Status = session.Status(status)
	sess.StatusDetail = detail.String
	sess.PID = int(pid.Int64)
	sess.SocketPath = socket.String
	if exitCode.Valid {
		code := int(exitCode.Int64)
		sess.ExitCode = &code
	}
	var err error
	if sess.CreatedAt, err = parseTime(createdAt.String); err != nil {
		return nil, err
	}
	if sess.UpdatedAt, err = parseTime(updated.String); err != nil {
		return nil, err
	}
	if endedAt.Valid {
		t, err := parseTime(endedAt.String)
		if err != nil {
			return nil, err
		}
		sess.EndedAt = &t
	}
	return &sess, nil
}

func fmtTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("corrupt timestamp %q: %w", s, err)
	}
	return t, nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func nullIntPtr(n *int) any {
	if n == nil {
		return nil
	}
	return *n
}

func nullTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return fmtTime(*t)
}
