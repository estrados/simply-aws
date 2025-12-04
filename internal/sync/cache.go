package sync

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const dbDir = ".saws"
const dbFile = ".saws/saws.db"

var db *sql.DB

func InitDB() error {
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return err
	}

	var err error
	db, err = sql.Open("sqlite3", dbFile+"?_journal_mode=WAL")
	if err != nil {
		return err
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS cache (
			key    TEXT PRIMARY KEY,
			value  TEXT NOT NULL,
			synced_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS settings (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS regions (
			name     TEXT PRIMARY KEY,
			enabled  INTEGER NOT NULL DEFAULT 1
		);
	`)
	return err
}

func WriteCache(key string, data []byte) error {
	_, err := db.Exec(
		`INSERT INTO cache (key, value, synced_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, synced_at=excluded.synced_at`,
		key, string(data), time.Now(),
	)
	return err
}

func ReadCache(key string) (json.RawMessage, error) {
	var value string
	err := db.QueryRow(`SELECT value FROM cache WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return json.RawMessage(value), nil
}

func CacheExists(key string) bool {
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM cache WHERE key = ?`, key).Scan(&count)
	return count > 0
}

type LastSync struct {
	Timestamp time.Time       `json:"timestamp"`
	Services  map[string]bool `json:"services"`
}

func WriteLastSync(services []string) error {
	ls := LastSync{
		Timestamp: time.Now(),
		Services:  make(map[string]bool),
	}
	for _, s := range services {
		ls.Services[s] = true
	}
	b, _ := json.Marshal(ls)
	return WriteCache("last_sync", b)
}

func ReadLastSync() (*LastSync, error) {
	data, err := ReadCache("last_sync")
	if err != nil || data == nil {
		return nil, err
	}
	var ls LastSync
	if err := json.Unmarshal(data, &ls); err != nil {
		return nil, err
	}
	return &ls, nil
}

// --- Region settings ---

func SetRegions(regions []string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Insert all regions, default enabled
	for _, r := range regions {
		_, err := tx.Exec(
			`INSERT INTO regions (name, enabled) VALUES (?, 1) ON CONFLICT(name) DO NOTHING`, r,
		)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func GetRegions() ([]RegionInfo, error) {
	rows, err := db.Query(`SELECT name, enabled FROM regions ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var regions []RegionInfo
	for rows.Next() {
		var r RegionInfo
		if err := rows.Scan(&r.Name, &r.Enabled); err != nil {
			return nil, err
		}
		regions = append(regions, r)
	}
	return regions, nil
}

func GetEnabledRegions() ([]string, error) {
	rows, err := db.Query(`SELECT name FROM regions WHERE enabled = 1 ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var regions []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		regions = append(regions, name)
	}
	return regions, nil
}

func SetRegionEnabled(name string, enabled bool) error {
	val := 0
	if enabled {
		val = 1
	}
	_, err := db.Exec(`UPDATE regions SET enabled = ? WHERE name = ?`, val, name)
	return err
}

type RegionInfo struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

func CloseDB() {
	if db != nil {
		db.Close()
	}
}

// DBPath returns the path to the db dir (for cleanup of old flat files).
func DBPath() string {
	abs, _ := filepath.Abs(dbDir)
	return abs
}
