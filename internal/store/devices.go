package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/evilgenius79/fable-tailscale/internal/model"
)

// UpsertDevices inserts or replaces the given devices in a single
// transaction. The full device is stored as a JSON document; name, online,
// first_seen, last_seen and updated_at are also kept as indexed columns.
// A zero FirstSeen keeps the previously stored value (or becomes now for a
// new row) and a zero UpdatedAt becomes now, so the returned documents always
// carry both timestamps.
func (s *Store) UpsertDevices(ctx context.Context, devs []model.Device) error {
	if len(devs) == 0 {
		return nil
	}
	for i, d := range devs {
		if d.ID == "" {
			return fmt.Errorf("store: upsert devices: device %d has empty id", i)
		}
	}
	now := s.now()
	return s.withTx(ctx, func(tx *sql.Tx) error {
		sel, err := tx.PrepareContext(ctx, `SELECT first_seen FROM devices WHERE id = ?`)
		if err != nil {
			return fmt.Errorf("store: prepare select: %w", err)
		}
		defer sel.Close()
		ins, err := tx.PrepareContext(ctx, `INSERT INTO devices(id, name, online, data, first_seen, last_seen, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				name = excluded.name,
				online = excluded.online,
				data = excluded.data,
				first_seen = excluded.first_seen,
				last_seen = excluded.last_seen,
				updated_at = excluded.updated_at`)
		if err != nil {
			return fmt.Errorf("store: prepare upsert: %w", err)
		}
		defer ins.Close()

		for _, d := range devs {
			if d.FirstSeen.IsZero() {
				var existing int64
				err := sel.QueryRowContext(ctx, string(d.ID)).Scan(&existing)
				switch {
				case err == nil && existing > 0:
					d.FirstSeen = fromUnix(existing)
				case err == nil || errors.Is(err, sql.ErrNoRows):
					d.FirstSeen = now
				default:
					return fmt.Errorf("store: lookup device %s: %w", d.ID, err)
				}
			}
			if d.UpdatedAt.IsZero() {
				d.UpdatedAt = now
			}
			data, err := json.Marshal(&d)
			if err != nil {
				return fmt.Errorf("store: encode device %s: %w", d.ID, err)
			}
			if _, err := ins.ExecContext(ctx, string(d.ID), d.Name, boolInt(d.Online), string(data),
				d.FirstSeen.Unix(), unixOrZero(d.LastSeen), d.UpdatedAt.Unix()); err != nil {
				return fmt.Errorf("store: upsert device %s: %w", d.ID, err)
			}
		}
		return nil
	})
}

// GetDevice returns the stored device with the given ID, or ErrNotFound.
func (s *Store) GetDevice(ctx context.Context, id model.DeviceID) (*model.Device, error) {
	var (
		data      string
		firstSeen int64
		updatedAt int64
	)
	err := s.db.QueryRowContext(ctx, `SELECT data, first_seen, updated_at FROM devices WHERE id = ?`, string(id)).
		Scan(&data, &firstSeen, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get device %s: %w", id, err)
	}
	d, err := decodeDevice(id, data, firstSeen, updatedAt)
	if err != nil {
		return nil, err
	}
	return d, nil
}

// ListDevices returns every stored device sorted by name (case-insensitive),
// then by ID. The result is never nil.
func (s *Store) ListDevices(ctx context.Context) ([]model.Device, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, data, first_seen, updated_at FROM devices ORDER BY name COLLATE NOCASE, id`)
	if err != nil {
		return nil, fmt.Errorf("store: list devices: %w", err)
	}
	defer rows.Close()
	out := []model.Device{}
	for rows.Next() {
		var (
			id        string
			data      string
			firstSeen int64
			updatedAt int64
		)
		if err := rows.Scan(&id, &data, &firstSeen, &updatedAt); err != nil {
			return nil, fmt.Errorf("store: list devices: scan: %w", err)
		}
		d, err := decodeDevice(model.DeviceID(id), data, firstSeen, updatedAt)
		if err != nil {
			// A corrupt document must not hide every other device.
			s.log.Warn("store: skipping undecodable device row", "id", id, "err", err)
			continue
		}
		out = append(out, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list devices: %w", err)
	}
	return out, nil
}

// DeleteDevice removes a device and its raw samples and rollups. Events,
// alerts and audit entries that reference the device are kept as history.
// It returns ErrNotFound when no such device exists.
func (s *Store) DeleteDevice(ctx context.Context, id model.DeviceID) error {
	if id == "" {
		return ErrNotFound
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM devices WHERE id = ?`, string(id))
		if err != nil {
			return fmt.Errorf("store: delete device %s: %w", id, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("store: delete device %s: %w", id, err)
		}
		if n == 0 {
			return ErrNotFound
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM samples WHERE device_id = ?`, string(id)); err != nil {
			return fmt.Errorf("store: delete samples of %s: %w", id, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM rollups WHERE device_id = ?`, string(id)); err != nil {
			return fmt.Errorf("store: delete rollups of %s: %w", id, err)
		}
		return nil
	})
}

// decodeDevice unmarshals a stored device document and fills in the
// timestamps from the indexed columns when the document lacks them.
func decodeDevice(id model.DeviceID, data string, firstSeen, updatedAt int64) (*model.Device, error) {
	var d model.Device
	if err := json.Unmarshal([]byte(data), &d); err != nil {
		return nil, fmt.Errorf("store: decode device %s: %w", id, err)
	}
	if d.ID == "" {
		d.ID = id
	}
	if d.FirstSeen.IsZero() && firstSeen > 0 {
		d.FirstSeen = fromUnix(firstSeen)
	}
	if d.UpdatedAt.IsZero() && updatedAt > 0 {
		d.UpdatedAt = fromUnix(updatedAt)
	}
	return &d, nil
}
