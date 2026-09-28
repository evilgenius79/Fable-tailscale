package store

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
)

// TestConcurrentAccess hammers the store from many goroutines with a mix of
// reads and writes on both an in-memory and a file database. Run with -race.
func TestConcurrentAccess(t *testing.T) {
	for _, tc := range []struct{ name, path string }{
		{"memory", ""},
		{"file", filepath.Join(t.TempDir(), "concurrent.db")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t, tc.path)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			const workers = 8
			const iterations = 10
			errs := make(chan error, workers*iterations*4)
			var wg sync.WaitGroup
			for w := 0; w < workers; w++ {
				wg.Add(1)
				go func(w int) {
					defer wg.Done()
					id := model.DeviceID(fmt.Sprintf("dev-%d", w))
					for i := 0; i < iterations; i++ {
						ts := at(int64(i * 15))
						if err := s.UpsertDevices(ctx, []model.Device{{ID: id, Name: string(id), Online: i%2 == 0}}); err != nil {
							errs <- err
						}
						if err := s.InsertSamples(ctx, []model.Sample{
							{DeviceID: id, TS: ts, Online: i%3 != 0, CPU: fp(float64(i)), LatencyMs: fp(1)},
							{DeviceID: id, TS: ts.Add(5 * time.Second), Online: true},
						}); err != nil {
							errs <- err
						}
						switch i % 6 {
						case 0:
							if _, err := s.ListDevices(ctx); err != nil {
								errs <- err
							}
						case 1:
							if _, err := s.QuerySeries(ctx, id, at(0), at(3600)); err != nil {
								errs <- err
							}
						case 2:
							if _, err := s.NetworkSparklines(ctx, at(0), at(600), time.Minute); err != nil {
								errs <- err
							}
						case 3:
							if _, err := s.Rollup(ctx, at(int64(i*15))); err != nil {
								errs <- err
							}
						case 4:
							if err := s.InsertEvent(ctx, &model.Event{Type: model.EventDeviceOnline, DeviceID: id}); err != nil {
								errs <- err
							}
							if _, err := s.ListEvents(ctx, model.EventQuery{DeviceID: id, Limit: 5}); err != nil {
								errs <- err
							}
						case 5:
							if _, err := s.UptimeRatios(ctx, at(0)); err != nil {
								errs <- err
							}
							if _, err := s.OnlineTimeline(ctx, id, at(0), at(600)); err != nil {
								errs <- err
							}
							if _, err := s.Stats(ctx); err != nil {
								errs <- err
							}
						}
					}
				}(w)
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				t.Errorf("concurrent op failed: %v", err)
			}

			devs, err := s.ListDevices(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(devs) != workers {
				t.Errorf("devices = %d, want %d", len(devs), workers)
			}
			st, err := s.Stats(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if st.Samples != int64(workers*iterations*2) {
				t.Errorf("samples = %d, want %d", st.Samples, workers*iterations*2)
			}
		})
	}
}

// TestConcurrentSameKeyWrites checks that writers racing on the same rows
// (same device, same kv key, same sample second) serialize cleanly.
func TestConcurrentSameKeyWrites(t *testing.T) {
	s := newTestStore(t, "")
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 200)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := s.SetKV(ctx, "shared", fmt.Sprint(i)); err != nil {
				errs <- err
			}
			if err := s.InsertSamples(ctx, []model.Sample{{DeviceID: "same", TS: at(0), CPU: fp(float64(i))}}); err != nil {
				errs <- err
			}
			if err := s.UpsertDevices(ctx, []model.Device{{ID: "same", Name: fmt.Sprint(i)}}); err != nil {
				errs <- err
			}
			if _, _, err := s.GetKV(ctx, "shared"); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("write failed: %v", err)
	}
	st, err := s.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Samples != 1 || st.Devices != 1 {
		t.Errorf("stats = %+v, want 1 sample / 1 device", st)
	}
}
