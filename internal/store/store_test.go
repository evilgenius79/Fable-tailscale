package store

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
)

// base is a fixed unix timestamp aligned to 3600s (and therefore to every
// smaller step used by the store) so bucket boundaries are predictable.
const base int64 = 1_700_006_400

// fixedNow is the deterministic clock used by tests that depend on "now".
var fixedNow = time.Unix(base+86_400*30, 0).UTC()

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestStore opens a store at path (":memory:" when empty) with a fixed
// clock and closes it when the test ends.
func newTestStore(t *testing.T, path string) *Store {
	t.Helper()
	if path == "" {
		path = ":memory:"
	}
	s, err := Open(context.Background(), path, testLogger())
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	s.now = func() time.Time { return fixedNow }
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func fp(v float64) *float64 { return &v }
func bp(v bool) *bool       { return &v }

func at(offset int64) time.Time { return time.Unix(base+offset, 0).UTC() }

func TestOpenMemoryAndFile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	tests := []struct {
		name string
		path string
		file bool
	}{
		{"memory", ":memory:", false},
		{"file in new nested dir", filepath.Join(dir, "a", "b", "tailwatch.db"), true},
		{"file with odd chars", filepath.Join(dir, "odd dir#1", "t?w%.db"), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Open(ctx, tc.path, testLogger())
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer s.Close()

			var journal string
			if err := s.db.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&journal); err != nil {
				t.Fatal(err)
			}
			var busy, fk int
			if err := s.db.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&busy); err != nil {
				t.Fatal(err)
			}
			if err := s.db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk); err != nil {
				t.Fatal(err)
			}
			if busy != 5000 || fk != 1 {
				t.Errorf("pragmas: busy_timeout=%d foreign_keys=%d", busy, fk)
			}
			if tc.file {
				if journal != "wal" {
					t.Errorf("journal_mode = %q, want wal", journal)
				}
				if _, err := os.Stat(tc.path); err != nil {
					t.Errorf("db file not created: %v", err)
				}
				if runtime.GOOS != "windows" {
					fi, err := os.Stat(filepath.Dir(tc.path))
					if err != nil {
						t.Fatal(err)
					}
					if perm := fi.Mode().Perm(); perm != 0o700 {
						t.Errorf("parent dir perm = %o, want 700", perm)
					}
				}
			}
			// Basic round trip through every table proves the schema exists.
			if err := s.SetKV(ctx, "k", "v"); err != nil {
				t.Fatal(err)
			}
			v, ok, err := s.GetKV(ctx, "k")
			if err != nil || !ok || v != "v" {
				t.Errorf("GetKV = %q, %v, %v", v, ok, err)
			}
			if _, _, err := s.GetKV(ctx, "missing"); err != nil {
				t.Errorf("GetKV missing: %v", err)
			}
			if err := s.Close(); err != nil {
				t.Errorf("Close: %v", err)
			}
			if err := s.Close(); err != nil {
				t.Errorf("second Close: %v", err)
			}
		})
	}
}

func TestOpenErrors(t *testing.T) {
	ctx := context.Background()
	if _, err := Open(ctx, "", nil); err == nil {
		t.Error("Open(\"\") succeeded, want error")
	}
	if runtime.GOOS != "windows" {
		// A path whose parent is a regular file cannot be created.
		f := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(ctx, filepath.Join(f, "sub", "db"), nil); err == nil {
			t.Error("Open under a regular file succeeded, want error")
		}
	}
}

func TestMigrationsIdempotentOnReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tailwatch.db")

	s, err := Open(ctx, path, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertDevices(ctx, []model.Device{{ID: "dev1", Name: "one"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		s, err = Open(ctx, path, testLogger())
		if err != nil {
			t.Fatalf("reopen %d: %v", i, err)
		}
		var n, v int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), MAX(version) FROM schema_version`).Scan(&n, &v); err != nil {
			t.Fatal(err)
		}
		if n != len(migrations) || v != migrations[len(migrations)-1].version {
			t.Errorf("reopen %d: schema_version rows=%d max=%d, want %d/%d", i, n, v, len(migrations), migrations[len(migrations)-1].version)
		}
		devs, err := s.ListDevices(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(devs) != 1 || devs[0].ID != "dev1" {
			t.Errorf("reopen %d: data not persisted: %+v", i, devs)
		}
		s.Close()
	}

	// A database from a newer release must be refused, not silently used.
	s, err = Open(ctx, path, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO schema_version(version, applied_at) VALUES (999, 0)`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := Open(ctx, path, testLogger()); err == nil {
		t.Error("Open with newer schema version succeeded, want error")
	}
}

func TestKV(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t, "")
	if err := s.SetKV(ctx, "", "x"); err == nil {
		t.Error("SetKV with empty key succeeded")
	}
	if err := s.SetKV(ctx, "a", "1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetKV(ctx, "a", "2"); err != nil {
		t.Fatal(err)
	}
	v, ok, err := s.GetKV(ctx, "a")
	if err != nil || !ok || v != "2" {
		t.Errorf("GetKV after overwrite = %q,%v,%v", v, ok, err)
	}
	v, ok, err = s.GetKV(ctx, "b")
	if err != nil || ok || v != "" {
		t.Errorf("GetKV missing = %q,%v,%v", v, ok, err)
	}
}

func TestStats(t *testing.T) {
	ctx := context.Background()
	for _, path := range []string{"", filepath.Join(t.TempDir(), "s.db")} {
		s := newTestStore(t, path)
		st, err := s.Stats(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st.Devices != 0 || st.Samples != 0 || st.OldestSample != nil {
			t.Errorf("empty stats = %+v", st)
		}
		if err := s.UpsertDevices(ctx, []model.Device{{ID: "d1"}, {ID: "d2"}}); err != nil {
			t.Fatal(err)
		}
		if err := s.InsertSamples(ctx, []model.Sample{{DeviceID: "d1", TS: at(30)}, {DeviceID: "d1", TS: at(0)}}); err != nil {
			t.Fatal(err)
		}
		if err := s.InsertEvent(ctx, &model.Event{Type: model.EventHubStarted}); err != nil {
			t.Fatal(err)
		}
		if err := s.OpenAlert(ctx, &model.Alert{RuleID: "r"}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Rollup(ctx, at(600)); err != nil {
			t.Fatal(err)
		}
		st, err = s.Stats(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st.Devices != 2 || st.Samples != 2 || st.Rollups != 1 || st.Events != 1 || st.Alerts != 1 {
			t.Errorf("stats = %+v", st)
		}
		if st.OldestSample == nil || !st.OldestSample.Equal(at(0)) {
			t.Errorf("oldest sample = %v, want %v", st.OldestSample, at(0))
		}
		if path == "" && st.SizeBytes != 0 {
			t.Errorf("memory size = %d, want 0", st.SizeBytes)
		}
		if path != "" && st.SizeBytes <= 0 {
			t.Errorf("file size = %d, want > 0", st.SizeBytes)
		}
	}
}

func TestClampLimit(t *testing.T) {
	tests := []struct{ in, def, max, want int }{
		{0, 100, 1000, 100},
		{-5, 100, 1000, 100},
		{7, 100, 1000, 7},
		{1000, 100, 1000, 1000},
		{5000, 100, 1000, 1000},
	}
	for _, tc := range tests {
		if got := clampLimit(tc.in, tc.def, tc.max); got != tc.want {
			t.Errorf("clampLimit(%d,%d,%d) = %d, want %d", tc.in, tc.def, tc.max, got, tc.want)
		}
	}
}

func TestContextCancelled(t *testing.T) {
	s := newTestStore(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.InsertSamples(ctx, []model.Sample{{DeviceID: "d", TS: at(0)}}); err == nil {
		t.Error("InsertSamples with cancelled context succeeded")
	} else if !errors.Is(err, context.Canceled) {
		t.Logf("InsertSamples error (not wrapping context.Canceled, acceptable): %v", err)
	}
	// The store must remain usable afterwards.
	if err := s.InsertSamples(context.Background(), []model.Sample{{DeviceID: "d", TS: at(0)}}); err != nil {
		t.Errorf("store unusable after cancelled call: %v", err)
	}
}
