package store

// migration is one schema version: a list of statements executed in a single
// transaction, after which the version is recorded in schema_version.
type migration struct {
	version    int
	statements []string
}

// migrations is the ordered list of schema versions. Append new versions;
// never edit an existing one once it has shipped.
var migrations = []migration{
	{version: 1, statements: schemaV1},
}

// schemaV1 is the full initial schema described in docs/ARCHITECTURE.md.
// All time columns are unix seconds (INTEGER); JSON columns are TEXT.
var schemaV1 = []string{
	`CREATE TABLE IF NOT EXISTS devices (
		id         TEXT PRIMARY KEY,
		name       TEXT NOT NULL DEFAULT '',
		online     INTEGER NOT NULL DEFAULT 0,
		data       TEXT NOT NULL,
		first_seen INTEGER NOT NULL DEFAULT 0,
		last_seen  INTEGER NOT NULL DEFAULT 0,
		updated_at INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE INDEX IF NOT EXISTS devices_name ON devices(name)`,

	`CREATE TABLE IF NOT EXISTS samples (
		device_id   TEXT NOT NULL,
		ts          INTEGER NOT NULL,
		online      INTEGER NOT NULL DEFAULT 0,
		latency_ms  REAL,
		relay       TEXT,
		direct      INTEGER,
		ts_rx_bytes INTEGER NOT NULL DEFAULT 0,
		ts_tx_bytes INTEGER NOT NULL DEFAULT 0,
		ts_rx_rate  REAL NOT NULL DEFAULT 0,
		ts_tx_rate  REAL NOT NULL DEFAULT 0,
		agent_ok    INTEGER NOT NULL DEFAULT 0,
		cpu         REAL,
		mem         REAL,
		disk        REAL,
		load1       REAL,
		net_rx_rate REAL,
		net_tx_rate REAL,
		temp_c      REAL,
		uptime_s    INTEGER,
		PRIMARY KEY (device_id, ts)
	) WITHOUT ROWID`,
	`CREATE INDEX IF NOT EXISTS samples_ts ON samples(ts)`,

	`CREATE TABLE IF NOT EXISTS rollups (
		device_id       TEXT NOT NULL,
		bucket          INTEGER NOT NULL,
		step            INTEGER NOT NULL,
		samples         INTEGER NOT NULL,
		online_ratio    REAL NOT NULL DEFAULT 0,
		direct_ratio    REAL,
		latency_avg     REAL,
		latency_max     REAL,
		ts_rx_rate_avg  REAL NOT NULL DEFAULT 0,
		ts_tx_rate_avg  REAL NOT NULL DEFAULT 0,
		cpu_avg         REAL,
		cpu_max         REAL,
		mem_avg         REAL,
		disk_avg        REAL,
		load1_avg       REAL,
		net_rx_rate_avg REAL,
		net_tx_rate_avg REAL,
		temp_avg        REAL,
		PRIMARY KEY (device_id, bucket)
	) WITHOUT ROWID`,
	`CREATE INDEX IF NOT EXISTS rollups_bucket ON rollups(bucket)`,

	`CREATE TABLE IF NOT EXISTS events (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		ts          INTEGER NOT NULL,
		type        TEXT NOT NULL,
		severity    TEXT NOT NULL DEFAULT 'info',
		device_id   TEXT NOT NULL DEFAULT '',
		device_name TEXT NOT NULL DEFAULT '',
		title       TEXT NOT NULL DEFAULT '',
		message     TEXT NOT NULL DEFAULT '',
		data        TEXT
	)`,
	`CREATE INDEX IF NOT EXISTS events_ts ON events(ts)`,
	`CREATE INDEX IF NOT EXISTS events_device_ts ON events(device_id, ts)`,

	`CREATE TABLE IF NOT EXISTS alerts (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		rule_id     TEXT NOT NULL,
		rule_type   TEXT NOT NULL DEFAULT '',
		device_id   TEXT NOT NULL DEFAULT '',
		device_name TEXT NOT NULL DEFAULT '',
		state       TEXT NOT NULL,
		severity    TEXT NOT NULL DEFAULT 'info',
		title       TEXT NOT NULL DEFAULT '',
		message     TEXT NOT NULL DEFAULT '',
		value       REAL,
		opened_at   INTEGER NOT NULL,
		updated_at  INTEGER NOT NULL,
		resolved_at INTEGER,
		acked_at    INTEGER,
		acked_by    TEXT NOT NULL DEFAULT '',
		data        TEXT
	)`,
	`CREATE INDEX IF NOT EXISTS alerts_state ON alerts(state)`,
	`CREATE INDEX IF NOT EXISTS alerts_device ON alerts(device_id, opened_at)`,

	`CREATE TABLE IF NOT EXISTS alert_rules (
		id         TEXT PRIMARY KEY,
		data       TEXT NOT NULL,
		updated_at INTEGER NOT NULL
	)`,

	`CREATE TABLE IF NOT EXISTS audit (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		ts         INTEGER NOT NULL,
		actor      TEXT NOT NULL DEFAULT '',
		actor_node TEXT NOT NULL DEFAULT '',
		action     TEXT NOT NULL DEFAULT '',
		target     TEXT NOT NULL DEFAULT '',
		details    TEXT,
		ok         INTEGER NOT NULL DEFAULT 0,
		error      TEXT NOT NULL DEFAULT '',
		remote_ip  TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE INDEX IF NOT EXISTS audit_ts ON audit(ts)`,

	`CREATE TABLE IF NOT EXISTS kv (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`,
}
