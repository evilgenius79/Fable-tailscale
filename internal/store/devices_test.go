package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
)

func TestDevicesCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t, "")

	lat := 12.5
	devs := []model.Device{
		{ID: "n3", Name: "zeta", Hostname: "zeta-host", OS: "linux", Addresses: []string{"100.64.0.3"}, Tags: []string{"tag:server"},
			Online: true, LastSeen: at(100), Connectivity: model.Connectivity{Path: model.PathDirect, LatencyMs: &lat, RxBytes: 10},
			Metrics: &model.MetricsSnapshot{CPUPercent: 42, Disks: []model.DiskUsage{{Mount: "/", Percent: 50}}}},
		{ID: "n1", Name: "Alpha", OS: "macOS", Online: false, LastSeen: at(50)},
		{ID: "n2", Name: "beta", OS: "windows", Online: true, FirstSeen: at(-1000), UpdatedAt: at(5)},
	}
	if err := s.UpsertDevices(ctx, devs); err != nil {
		t.Fatalf("UpsertDevices: %v", err)
	}

	got, err := s.GetDevice(ctx, "n3")
	if err != nil {
		t.Fatalf("GetDevice: %v", err)
	}
	if got.Name != "zeta" || got.Hostname != "zeta-host" || len(got.Tags) != 1 || got.Connectivity.LatencyMs == nil || *got.Connectivity.LatencyMs != 12.5 {
		t.Errorf("GetDevice round trip mismatch: %+v", got)
	}
	if got.Metrics == nil || got.Metrics.CPUPercent != 42 || len(got.Metrics.Disks) != 1 {
		t.Errorf("metrics not round-tripped: %+v", got.Metrics)
	}
	if !got.FirstSeen.Equal(fixedNow) || !got.UpdatedAt.Equal(fixedNow) {
		t.Errorf("zero FirstSeen/UpdatedAt should default to now: first=%v updated=%v", got.FirstSeen, got.UpdatedAt)
	}
	n2, err := s.GetDevice(ctx, "n2")
	if err != nil {
		t.Fatal(err)
	}
	if !n2.FirstSeen.Equal(at(-1000)) || !n2.UpdatedAt.Equal(at(5)) {
		t.Errorf("explicit timestamps not kept: first=%v updated=%v", n2.FirstSeen, n2.UpdatedAt)
	}

	list, err := s.ListDevices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wantOrder := []model.DeviceID{"n1", "n2", "n3"} // Alpha, beta, zeta (case-insensitive)
	if len(list) != 3 {
		t.Fatalf("ListDevices len = %d, want 3", len(list))
	}
	for i, id := range wantOrder {
		if list[i].ID != id {
			t.Errorf("ListDevices[%d] = %s, want %s", i, list[i].ID, id)
		}
	}

	// Re-upsert with zero FirstSeen keeps the stored value; the rest updates.
	if err := s.UpsertDevices(ctx, []model.Device{{ID: "n2", Name: "beta-renamed", Online: false}}); err != nil {
		t.Fatal(err)
	}
	n2, err = s.GetDevice(ctx, "n2")
	if err != nil {
		t.Fatal(err)
	}
	if n2.Name != "beta-renamed" || n2.Online || !n2.FirstSeen.Equal(at(-1000)) {
		t.Errorf("re-upsert: %+v", n2)
	}
	var online int
	if err := s.db.QueryRowContext(ctx, `SELECT online FROM devices WHERE id = 'n2'`).Scan(&online); err != nil {
		t.Fatal(err)
	}
	if online != 0 {
		t.Errorf("online column not updated: %d", online)
	}

	// Delete removes the device, its samples and rollups but keeps events.
	if err := s.InsertSamples(ctx, []model.Sample{{DeviceID: "n2", TS: at(0)}, {DeviceID: "n3", TS: at(0)}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rollup(ctx, at(1000)); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertEvent(ctx, &model.Event{Type: model.EventDeviceOffline, DeviceID: "n2"}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteDevice(ctx, "n2"); err != nil {
		t.Fatalf("DeleteDevice: %v", err)
	}
	if _, err := s.GetDevice(ctx, "n2"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetDevice after delete err = %v, want ErrNotFound", err)
	}
	if err := s.DeleteDevice(ctx, "n2"); !errors.Is(err, ErrNotFound) {
		t.Errorf("second DeleteDevice err = %v, want ErrNotFound", err)
	}
	if _, err := s.LatestSample(ctx, "n2"); !errors.Is(err, ErrNotFound) {
		t.Errorf("samples of deleted device survived: %v", err)
	}
	if _, err := s.LatestSample(ctx, "n3"); err != nil {
		t.Errorf("samples of other device lost: %v", err)
	}
	var rollups int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM rollups WHERE device_id = 'n2'`).Scan(&rollups); err != nil {
		t.Fatal(err)
	}
	if rollups != 0 {
		t.Errorf("rollups of deleted device survived: %d", rollups)
	}
	evs, err := s.ListEvents(ctx, model.EventQuery{DeviceID: "n2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Errorf("events of deleted device = %d, want 1 (kept as history)", len(evs))
	}
}

func TestDevicesEdgeCases(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t, "")

	if err := s.UpsertDevices(ctx, nil); err != nil {
		t.Errorf("UpsertDevices(nil) = %v", err)
	}
	if err := s.UpsertDevices(ctx, []model.Device{{ID: "ok"}, {ID: ""}}); err == nil {
		t.Error("UpsertDevices with empty id succeeded")
	}
	if _, err := s.GetDevice(ctx, "ok"); !errors.Is(err, ErrNotFound) {
		t.Errorf("batch with bad device was partially applied: %v", err)
	}
	if _, err := s.GetDevice(ctx, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetDevice(\"\") = %v", err)
	}
	if err := s.DeleteDevice(ctx, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteDevice(\"\") = %v", err)
	}
	list, err := s.ListDevices(ctx)
	if err != nil || list == nil || len(list) != 0 {
		t.Errorf("ListDevices empty = %v, %v", list, err)
	}

	// A corrupt document is skipped by ListDevices and reported by GetDevice.
	if _, err := s.db.ExecContext(ctx, `INSERT INTO devices(id, name, data, first_seen, last_seen, updated_at) VALUES ('bad', 'bad', '{not json', 1, 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertDevices(ctx, []model.Device{{ID: "good", Name: "good"}}); err != nil {
		t.Fatal(err)
	}
	list, err = s.ListDevices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != "good" {
		t.Errorf("ListDevices with corrupt row = %+v", list)
	}
	if _, err := s.GetDevice(ctx, "bad"); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("GetDevice(bad) = %v, want decode error", err)
	}

	// Timestamps stored in JSON keep sub-second precision.
	ts := time.Date(2026, 1, 2, 3, 4, 5, 600_000_000, time.UTC)
	if err := s.UpsertDevices(ctx, []model.Device{{ID: "ts", FirstSeen: ts, UpdatedAt: ts, LastSeen: ts}}); err != nil {
		t.Fatal(err)
	}
	d, err := s.GetDevice(ctx, "ts")
	if err != nil {
		t.Fatal(err)
	}
	if !d.FirstSeen.Equal(ts) || !d.LastSeen.Equal(ts) {
		t.Errorf("timestamps lost precision: %v %v", d.FirstSeen, d.LastSeen)
	}
}
