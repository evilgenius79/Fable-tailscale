// Package store is the SQLite persistence layer of the Tailwatch hub. It
// stores devices (as JSON documents with a few indexed columns), raw
// per-tick samples, 5-minute rollups, events, alerts, alert rules, the audit
// log and a small key/value table.
//
// The database is opened with modernc.org/sqlite (pure Go, no cgo). File
// databases use WAL journaling with a small connection pool; ":memory:"
// databases are pinned to a single connection so the in-memory database is
// shared by every query. All write transactions are serialized in-process
// through a mutex and begin with BEGIN IMMEDIATE, so concurrent use from many
// goroutines is safe.
//
// All time columns are unix seconds (INTEGER); JSON columns are TEXT.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"

	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver
)

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("store: not found")

const (
	// rawIntervalSec is the assumed spacing of raw samples (collector poll
	// interval) used for gap detection and the native series step.
	rawIntervalSec int64 = 15
	// rollupStepSec is the bucket width of rows in the rollups table.
	rollupStepSec int64 = 300
	// kvRollupWatermark is the kv key holding the unix second up to which raw
	// samples have been rolled up (an optimization only).
	kvRollupWatermark = "rollup_watermark"
	// vacuumThreshold is the number of deleted rows above which Prune runs
	// VACUUM to return space to the file system.
	vacuumThreshold = 100_000
	// filePoolSize is the connection pool size for file-backed databases.
	filePoolSize = 4
)

// dsnParams are the query parameters appended to every DSN. busy_timeout is
// applied first by the driver; the others make WAL + NORMAL sync the default
// for file databases (in-memory databases silently keep their own journal
// mode). _txlock=immediate makes every transaction take the write lock up
// front so writers never deadlock upgrading a read lock.
const dsnParams = "_pragma=busy_timeout(5000)" +
	"&_pragma=journal_mode(WAL)" +
	"&_pragma=foreign_keys(ON)" +
	"&_pragma=synchronous(NORMAL)" +
	"&_txlock=immediate"

// Store is a SQLite-backed persistence layer. It is safe for concurrent use.
type Store struct {
	db   *sql.DB
	path string // absolute file path, or "" for an in-memory database
	log  *slog.Logger

	// wmu serializes write transactions (and VACUUM) in-process. SQLite only
	// allows one writer at a time anyway; serializing here avoids relying on
	// busy_timeout for contention between our own goroutines.
	wmu sync.Mutex

	// now is the clock; tests override it.
	now func() time.Time

	closeOnce sync.Once
	closeErr  error
}

// PruneResult reports how many rows Prune deleted from each table.
type PruneResult struct {
	Samples, Rollups, Events, Alerts int64
}

// Total returns the total number of deleted rows.
func (p PruneResult) Total() int64 { return p.Samples + p.Rollups + p.Events + p.Alerts }

// Open opens (creating if necessary) the database at path and applies any
// pending schema migrations. path may be ":memory:" for a private in-memory
// database, which is useful for tests and the demo. For file databases the
// parent directory is created with mode 0700, and the connection uses WAL
// journaling, busy_timeout=5000ms, foreign_keys=ON and synchronous=NORMAL.
// A nil log uses slog.Default().
func Open(ctx context.Context, path string, log *slog.Logger) (*Store, error) {
	if log == nil {
		log = slog.Default()
	}
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("store: empty database path")
	}

	var (
		dsn      string
		absPath  string
		inMemory = path == ":memory:"
	)
	if inMemory {
		dsn = ":memory:?" + dsnParams
	} else {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("store: resolve path %q: %w", path, err)
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
			return nil, fmt.Errorf("store: create data directory: %w", err)
		}
		absPath = abs
		dsn = fileDSN(abs)
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open database: %w", err)
	}
	if inMemory {
		// Every new connection to ":memory:" would be a fresh, empty
		// database, so pin the pool to exactly one connection that never
		// expires.
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	} else {
		db.SetMaxOpenConns(filePoolSize)
		db.SetMaxIdleConns(filePoolSize)
	}
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(0)

	s := &Store{db: db, path: absPath, log: log, now: time.Now}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: connect to database: %w", err)
	}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	var journal string
	if err := db.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&journal); err != nil {
		journal = "unknown"
	}
	log.Info("store: database open", "path", displayPath(absPath), "journal_mode", journal)
	return s, nil
}

// fileDSN builds a "file:" URI DSN for an absolute path, escaping the
// characters that would otherwise be interpreted as URI syntax.
func fileDSN(abs string) string {
	p := filepath.ToSlash(abs)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // e.g. Windows drive letters: file:/C:/data/tailwatch.db
	}
	r := strings.NewReplacer("%", "%25", "?", "%3F", "#", "%23")
	return "file:" + r.Replace(p) + "?" + dsnParams
}

func displayPath(p string) string {
	if p == "" {
		return ":memory:"
	}
	return p
}

// Close closes the database. It is safe to call more than once.
func (s *Store) Close() error {
	s.closeOnce.Do(func() {
		s.wmu.Lock()
		defer s.wmu.Unlock()
		if err := s.db.Close(); err != nil {
			s.closeErr = fmt.Errorf("store: close: %w", err)
		}
	})
	return s.closeErr
}

// migrate creates the schema_version table and applies every migration
// newer than the recorded version, each in its own transaction.
func (s *Store) migrate(ctx context.Context) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()

	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (
		version    INTEGER PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("store: create schema_version: %w", err)
	}
	var current int
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&current); err != nil {
		return fmt.Errorf("store: read schema version: %w", err)
	}
	latest := migrations[len(migrations)-1].version
	if current > latest {
		return fmt.Errorf("store: database schema version %d is newer than supported version %d", current, latest)
	}
	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		if err := s.applyMigration(ctx, m); err != nil {
			return fmt.Errorf("store: apply migration %d: %w", m.version, err)
		}
		s.log.Info("store: applied schema migration", "version", m.version)
	}
	return nil
}

func (s *Store) applyMigration(ctx context.Context, m migration) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	for _, stmt := range m.statements {
		if _, err = tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("exec %q: %w", firstLine(stmt), err)
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO schema_version(version, applied_at) VALUES (?, ?)`, m.version, s.now().Unix()); err != nil {
		return fmt.Errorf("record version: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// withTx runs fn inside a serialized write transaction. The transaction is
// committed when fn returns nil and rolled back otherwise. Code inside fn must
// only use tx (never s.db) so it cannot deadlock on a single-connection pool.
func (s *Store) withTx(ctx context.Context, fn func(tx *sql.Tx) error) (err error) {
	s.wmu.Lock()
	defer s.wmu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin transaction: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("store: commit transaction: %w", err)
	}
	return nil
}

// GetKV returns the value stored under key. The boolean reports whether the
// key exists.
func (s *Store) GetKV(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("store: get kv %q: %w", key, err)
	}
	return v, true, nil
}

// SetKV stores value under key, replacing any existing value.
func (s *Store) SetKV(ctx context.Context, key, value string) error {
	if key == "" {
		return errors.New("store: empty kv key")
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if err := setKVTx(ctx, tx, key, value); err != nil {
			return fmt.Errorf("store: set kv %q: %w", key, err)
		}
		return nil
	})
}

func setKVTx(ctx context.Context, tx *sql.Tx, key, value string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO kv(key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func getKVTx(ctx context.Context, tx *sql.Tx, key string) (string, bool, error) {
	var v string
	err := tx.QueryRowContext(ctx, `SELECT value FROM kv WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// Stats returns row counts, the on-disk size (0 for in-memory databases) and
// the timestamp of the oldest raw sample.
func (s *Store) Stats(ctx context.Context) (model.StoreStats, error) {
	var st model.StoreStats
	counts := []struct {
		table string
		dst   *int64
	}{
		{"devices", &st.Devices},
		{"samples", &st.Samples},
		{"rollups", &st.Rollups},
		{"events", &st.Events},
		{"alerts", &st.Alerts},
	}
	for _, c := range counts {
		// Table names come from the fixed list above, never from input.
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+c.table).Scan(c.dst); err != nil {
			return model.StoreStats{}, fmt.Errorf("store: count %s: %w", c.table, err)
		}
	}
	var oldest sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MIN(ts) FROM samples`).Scan(&oldest); err != nil {
		return model.StoreStats{}, fmt.Errorf("store: oldest sample: %w", err)
	}
	if oldest.Valid {
		t := fromUnix(oldest.Int64)
		st.OldestSample = &t
	}
	if s.path != "" {
		for _, suffix := range []string{"", "-wal"} {
			if fi, err := os.Stat(s.path + suffix); err == nil {
				st.SizeBytes += fi.Size()
			}
		}
	}
	return st, nil
}

// --- small conversion helpers -------------------------------------------

// unixOrZero converts t to unix seconds, mapping the zero time to 0.
func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

// fromUnix converts unix seconds to a UTC time.
func fromUnix(u int64) time.Time { return time.Unix(u, 0).UTC() }

// unixPtr converts an optional time to a nullable unix-seconds column.
func unixPtr(t *time.Time) sql.NullInt64 {
	if t == nil || t.IsZero() {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: t.Unix(), Valid: true}
}

// timePtr converts a nullable unix-seconds column to an optional time.
func timePtr(n sql.NullInt64) *time.Time {
	if !n.Valid {
		return nil
	}
	t := fromUnix(n.Int64)
	return &t
}

// floatPtr converts a nullable REAL column to *float64.
func floatPtr(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	v := n.Float64
	return &v
}

// nullFloat converts *float64 to a nullable REAL parameter.
func nullFloat(p *float64) sql.NullFloat64 {
	if p == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *p, Valid: true}
}

// nullBool converts *bool to a nullable INTEGER parameter (0/1).
func nullBool(p *bool) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: boolInt(*p), Valid: true}
}

// boolPtr converts a nullable INTEGER column to *bool.
func boolPtr(n sql.NullInt64) *bool {
	if !n.Valid {
		return nil
	}
	v := n.Int64 != 0
	return &v
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// nullString converts "" to NULL so empty optional text does not take space.
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// clampLimit applies default and maximum values to a caller-supplied limit.
func clampLimit(limit, def, max int) int {
	if limit <= 0 {
		return def
	}
	if limit > max {
		return max
	}
	return limit
}
