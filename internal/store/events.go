package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/evilgenius79/fable-tailscale/internal/model"
)

// --- events ---------------------------------------------------------------

// InsertEvent appends an event and sets e.ID. A zero TS becomes now and an
// empty severity becomes "info". Data is stored as JSON.
func (s *Store) InsertEvent(ctx context.Context, e *model.Event) error {
	if e == nil {
		return errors.New("store: insert event: nil event")
	}
	if e.TS.IsZero() {
		e.TS = s.now()
	}
	if e.Severity == "" {
		e.Severity = model.SeverityInfo
	}
	data, err := encodeJSONMap(e.Data)
	if err != nil {
		return fmt.Errorf("store: insert event: encode data: %w", err)
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO events(ts, type, severity, device_id, device_name, title, message, data)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			e.TS.Unix(), string(e.Type), string(e.Severity), string(e.DeviceID), e.Device, e.Title, e.Message, data)
		if err != nil {
			return fmt.Errorf("store: insert event: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("store: insert event: id: %w", err)
		}
		e.ID = id
		return nil
	})
}

// ListEvents returns events matching q, newest first. Limit defaults to 100
// and is capped at 1000. The result is never nil.
func (s *Store) ListEvents(ctx context.Context, q model.EventQuery) ([]model.Event, error) {
	var (
		where []string
		args  []any
	)
	if q.DeviceID != "" {
		where = append(where, "device_id = ?")
		args = append(args, string(q.DeviceID))
	}
	if len(q.Types) > 0 {
		ph := make([]string, len(q.Types))
		for i, t := range q.Types {
			ph[i] = "?"
			args = append(args, string(t))
		}
		where = append(where, "type IN ("+strings.Join(ph, ",")+")")
	}
	if !q.Since.IsZero() {
		where = append(where, "ts >= ?")
		args = append(args, q.Since.Unix())
	}
	if !q.Before.IsZero() {
		where = append(where, "ts < ?")
		args = append(args, q.Before.Unix())
	}
	query := `SELECT id, ts, type, severity, device_id, device_name, title, message, data FROM events`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY ts DESC, id DESC LIMIT ?"
	args = append(args, clampLimit(q.Limit, 100, 1000))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list events: %w", err)
	}
	defer rows.Close()
	out := []model.Event{}
	for rows.Next() {
		var (
			e                        model.Event
			ts                       int64
			typ, sev, devID, devName string
			data                     sql.NullString
		)
		if err := rows.Scan(&e.ID, &ts, &typ, &sev, &devID, &devName, &e.Title, &e.Message, &data); err != nil {
			return nil, fmt.Errorf("store: list events: scan: %w", err)
		}
		e.TS = fromUnix(ts)
		e.Type = model.EventType(typ)
		e.Severity = model.Severity(sev)
		e.DeviceID = model.DeviceID(devID)
		e.Device = devName
		e.Data = s.decodeJSONMap(data, "event", e.ID)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list events: %w", err)
	}
	return out, nil
}

// --- alerts ---------------------------------------------------------------

const alertColumns = `id, rule_id, rule_type, device_id, device_name, state, severity, title, message, value,
	opened_at, updated_at, resolved_at, acked_at, acked_by, data`

// OpenAlert inserts a new alert and sets a.ID. An empty State becomes
// "open", a zero OpenedAt becomes now and a zero UpdatedAt becomes OpenedAt.
func (s *Store) OpenAlert(ctx context.Context, a *model.Alert) error {
	if a == nil {
		return errors.New("store: open alert: nil alert")
	}
	if a.RuleID == "" {
		return errors.New("store: open alert: empty rule id")
	}
	if a.State == "" {
		a.State = model.AlertOpen
	}
	if a.OpenedAt.IsZero() {
		a.OpenedAt = s.now()
	}
	if a.UpdatedAt.IsZero() {
		a.UpdatedAt = a.OpenedAt
	}
	if a.Severity == "" {
		a.Severity = model.SeverityInfo
	}
	data, err := encodeJSONMap(a.Data)
	if err != nil {
		return fmt.Errorf("store: open alert: encode data: %w", err)
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO alerts(rule_id, rule_type, device_id, device_name, state, severity, title, message, value,
				opened_at, updated_at, resolved_at, acked_at, acked_by, data)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			a.RuleID, string(a.RuleType), string(a.DeviceID), a.DeviceName, string(a.State), string(a.Severity), a.Title, a.Message,
			nullFloat(a.Value), a.OpenedAt.Unix(), a.UpdatedAt.Unix(), unixPtr(a.ResolvedAt), unixPtr(a.AckedAt), a.AckedBy, data)
		if err != nil {
			return fmt.Errorf("store: open alert: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("store: open alert: id: %w", err)
		}
		a.ID = id
		return nil
	})
}

// UpdateAlert stores the mutable fields of an existing alert (state,
// severity, title, message, value, updated_at, resolved_at, acked_at,
// acked_by, data) by ID. A zero UpdatedAt becomes now; an empty State is an
// error. It returns ErrNotFound when the alert does not exist.
func (s *Store) UpdateAlert(ctx context.Context, a *model.Alert) error {
	if a == nil {
		return errors.New("store: update alert: nil alert")
	}
	if a.ID <= 0 {
		return ErrNotFound
	}
	if a.State == "" {
		return fmt.Errorf("store: update alert %d: empty state", a.ID)
	}
	if a.UpdatedAt.IsZero() {
		a.UpdatedAt = s.now()
	}
	data, err := encodeJSONMap(a.Data)
	if err != nil {
		return fmt.Errorf("store: update alert: encode data: %w", err)
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE alerts SET state = ?, severity = ?, title = ?, message = ?, value = ?,
				updated_at = ?, resolved_at = ?, acked_at = ?, acked_by = ?, data = ?
			WHERE id = ?`,
			string(a.State), string(a.Severity), a.Title, a.Message, nullFloat(a.Value),
			a.UpdatedAt.Unix(), unixPtr(a.ResolvedAt), unixPtr(a.AckedAt), a.AckedBy, data, a.ID)
		if err != nil {
			return fmt.Errorf("store: update alert %d: %w", a.ID, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("store: update alert %d: %w", a.ID, err)
		}
		if n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// GetAlert returns the alert with the given ID, or ErrNotFound.
func (s *Store) GetAlert(ctx context.Context, id int64) (*model.Alert, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+alertColumns+` FROM alerts WHERE id = ?`, id)
	a, err := s.scanAlert(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get alert %d: %w", id, err)
	}
	return a, nil
}

// ListAlerts returns alerts matching q, newest first. An empty State means
// all states. Limit defaults to 200 and is capped at 1000. The result is
// never nil.
func (s *Store) ListAlerts(ctx context.Context, q model.AlertQuery) ([]model.Alert, error) {
	var (
		where []string
		args  []any
	)
	if q.State != "" {
		where = append(where, "state = ?")
		args = append(args, string(q.State))
	}
	if q.DeviceID != "" {
		where = append(where, "device_id = ?")
		args = append(args, string(q.DeviceID))
	}
	query := `SELECT ` + alertColumns + ` FROM alerts`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY opened_at DESC, id DESC LIMIT ?"
	args = append(args, clampLimit(q.Limit, 200, 1000))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list alerts: %w", err)
	}
	defer rows.Close()
	out := []model.Alert{}
	for rows.Next() {
		a, err := s.scanAlert(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list alerts: scan: %w", err)
		}
		out = append(out, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list alerts: %w", err)
	}
	return out, nil
}

// OpenAlertsByKey returns every open alert keyed by Alert.Key()
// (rule id + device id). If duplicates exist the newest wins.
func (s *Store) OpenAlertsByKey(ctx context.Context) (map[string]*model.Alert, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+alertColumns+` FROM alerts WHERE state = ? ORDER BY id`, string(model.AlertOpen))
	if err != nil {
		return nil, fmt.Errorf("store: open alerts: %w", err)
	}
	defer rows.Close()
	out := map[string]*model.Alert{}
	for rows.Next() {
		a, err := s.scanAlert(rows)
		if err != nil {
			return nil, fmt.Errorf("store: open alerts: scan: %w", err)
		}
		out[a.Key()] = a
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: open alerts: %w", err)
	}
	return out, nil
}

func (s *Store) scanAlert(sc scanner) (*model.Alert, error) {
	var (
		a                           model.Alert
		ruleType, devID, state, sev string
		value                       sql.NullFloat64
		openedAt, updatedAt         int64
		resolvedAt, ackedAt         sql.NullInt64
		data                        sql.NullString
	)
	if err := sc.Scan(&a.ID, &a.RuleID, &ruleType, &devID, &a.DeviceName, &state, &sev, &a.Title, &a.Message, &value,
		&openedAt, &updatedAt, &resolvedAt, &ackedAt, &a.AckedBy, &data); err != nil {
		return nil, err
	}
	a.RuleType = model.AlertRuleType(ruleType)
	a.DeviceID = model.DeviceID(devID)
	a.State = model.AlertState(state)
	a.Severity = model.Severity(sev)
	a.Value = floatPtr(value)
	a.OpenedAt = fromUnix(openedAt)
	a.UpdatedAt = fromUnix(updatedAt)
	a.ResolvedAt = timePtr(resolvedAt)
	a.AckedAt = timePtr(ackedAt)
	a.Data = s.decodeJSONMap(data, "alert", a.ID)
	return &a, nil
}

// --- rules ----------------------------------------------------------------

// ListRules returns all stored alert rules sorted by ID. Rows that fail to
// decode are skipped with a warning so one bad row cannot disable the rule
// engine. The result is never nil.
func (s *Store) ListRules(ctx context.Context) ([]model.AlertRule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, data FROM alert_rules ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list rules: %w", err)
	}
	defer rows.Close()
	out := []model.AlertRule{}
	for rows.Next() {
		var id, data string
		if err := rows.Scan(&id, &data); err != nil {
			return nil, fmt.Errorf("store: list rules: scan: %w", err)
		}
		var r model.AlertRule
		if err := json.Unmarshal([]byte(data), &r); err != nil {
			s.log.Warn("store: skipping undecodable alert rule", "id", id, "err", err)
			continue
		}
		if r.ID == "" {
			r.ID = id
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list rules: %w", err)
	}
	return out, nil
}

// SaveRule inserts or replaces a rule keyed by r.ID and sets r.UpdatedAt to
// now.
func (s *Store) SaveRule(ctx context.Context, r *model.AlertRule) error {
	if r == nil {
		return errors.New("store: save rule: nil rule")
	}
	if strings.TrimSpace(r.ID) == "" {
		return errors.New("store: save rule: empty rule id")
	}
	r.UpdatedAt = s.now().UTC().Truncate(0)
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("store: save rule %s: encode: %w", r.ID, err)
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO alert_rules(id, data, updated_at) VALUES (?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET data = excluded.data, updated_at = excluded.updated_at`,
			r.ID, string(data), r.UpdatedAt.Unix()); err != nil {
			return fmt.Errorf("store: save rule %s: %w", r.ID, err)
		}
		return nil
	})
}

// --- audit ----------------------------------------------------------------

// InsertAudit appends an audit entry and sets a.ID. A zero TS becomes now.
func (s *Store) InsertAudit(ctx context.Context, a *model.AuditEntry) error {
	if a == nil {
		return errors.New("store: insert audit: nil entry")
	}
	if a.TS.IsZero() {
		a.TS = s.now()
	}
	details, err := encodeJSONMap(a.Details)
	if err != nil {
		return fmt.Errorf("store: insert audit: encode details: %w", err)
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO audit(ts, actor, actor_node, action, target, details, ok, error, remote_ip)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			a.TS.Unix(), a.Actor, a.ActorNode, a.Action, a.Target, details, boolInt(a.OK), a.Error, a.RemoteIP)
		if err != nil {
			return fmt.Errorf("store: insert audit: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("store: insert audit: id: %w", err)
		}
		a.ID = id
		return nil
	})
}

// ListAudit returns the newest audit entries. Limit defaults to 100 and is
// capped at 1000. The result is never nil.
func (s *Store) ListAudit(ctx context.Context, limit int) ([]model.AuditEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, ts, actor, actor_node, action, target, details, ok, error, remote_ip
		FROM audit ORDER BY ts DESC, id DESC LIMIT ?`, clampLimit(limit, 100, 1000))
	if err != nil {
		return nil, fmt.Errorf("store: list audit: %w", err)
	}
	defer rows.Close()
	out := []model.AuditEntry{}
	for rows.Next() {
		var (
			a       model.AuditEntry
			ts, ok  int64
			details sql.NullString
		)
		if err := rows.Scan(&a.ID, &ts, &a.Actor, &a.ActorNode, &a.Action, &a.Target, &details, &ok, &a.Error, &a.RemoteIP); err != nil {
			return nil, fmt.Errorf("store: list audit: scan: %w", err)
		}
		a.TS = fromUnix(ts)
		a.OK = ok != 0
		a.Details = s.decodeJSONMap(details, "audit", a.ID)
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list audit: %w", err)
	}
	return out, nil
}

// --- JSON helpers ---------------------------------------------------------

// encodeJSONMap encodes an optional map as a nullable JSON column (empty
// maps become NULL).
func encodeJSONMap(m map[string]any) (sql.NullString, error) {
	if len(m) == 0 {
		return sql.NullString{}, nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return sql.NullString{}, err
	}
	return sql.NullString{String: string(b), Valid: true}, nil
}

// decodeJSONMap decodes a nullable JSON column; undecodable data is logged
// and yields nil rather than failing the whole query.
func (s *Store) decodeJSONMap(v sql.NullString, kind string, id int64) map[string]any {
	if !v.Valid || v.String == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(v.String), &m); err != nil {
		s.log.Warn("store: undecodable JSON column", "kind", kind, "id", id, "err", err)
		return nil
	}
	return m
}
