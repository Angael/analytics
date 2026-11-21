package main

import (
	"context"
	"database/sql"
	"log"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// event is one row. Strings are interned into the `str` table so rows stay ~30-50 bytes.
type event struct {
	ts      int64
	site    string
	typ     int // 0 = pageview, 1 = custom event
	name    string
	path    string
	ref     string
	props   string
	visitor int64
	country string
	device  int
	browser int
	os      int
	ver     string // app/release version reported by the client (optional)
}

const schema = `
PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS str (id INTEGER PRIMARY KEY, s TEXT NOT NULL UNIQUE);
CREATE TABLE IF NOT EXISTS ev (
	ts INTEGER NOT NULL,
	site INTEGER NOT NULL,
	typ INTEGER NOT NULL,
	name INTEGER,
	path INTEGER,
	ref INTEGER,
	props INTEGER,
	visitor INTEGER NOT NULL,
	country TEXT,
	device INTEGER NOT NULL,
	browser INTEGER NOT NULL,
	os INTEGER NOT NULL,
	ver INTEGER
);
CREATE INDEX IF NOT EXISTS ev_site_ts ON ev(site, ts);
CREATE TABLE IF NOT EXISTS kv (k TEXT PRIMARY KEY, v BLOB);
`

type store struct {
	db    *sql.DB
	in    chan event
	mu    sync.Mutex
	cache map[string]int64 // interned strings
}

func openStore(path string) (*store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_txlock=immediate")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		return nil, err
	}
	return &store{db: db, in: make(chan event, 4096), cache: map[string]int64{}}, nil
}

// migrate upgrades databases created by older versions in place. Steps must be idempotent.
func migrate(db *sql.DB) error {
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info('ev') WHERE name = 'ver'`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		if _, err := db.Exec(`ALTER TABLE ev ADD COLUMN ver INTEGER`); err != nil {
			return err
		}
	}
	return nil
}

func (s *store) close() { s.db.Close() }

// push enqueues an event without blocking; drops if the queue is full.
func (s *store) push(e event) {
	select {
	case s.in <- e:
	default:
		log.Println("queue full, dropping event")
	}
}

// writer batches inserts: flushes every second or every 500 events.
func (s *store) writer(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	buf := make([]event, 0, 500)
	flush := func() {
		if len(buf) == 0 {
			return
		}
		if err := s.insert(buf); err != nil {
			log.Println("insert:", err)
		}
		buf = buf[:0]
	}
	for {
		select {
		case e := <-s.in:
			buf = append(buf, e)
			if len(buf) >= 500 {
				flush()
			}
		case <-t.C:
			flush()
		case <-ctx.Done():
			for {
				select {
				case e := <-s.in:
					buf = append(buf, e)
				default:
					flush()
					return
				}
			}
		}
	}
}

func (s *store) insert(evs []event) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT INTO ev (ts, site, typ, name, path, ref, props, visitor, country, device, browser, os, ver) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, e := range evs {
		ids := [6]any{}
		for i, v := range [6]string{e.site, e.name, e.path, e.ref, e.props, e.ver} {
			if v == "" {
				continue
			}
			id, err := s.intern(tx, v)
			if err != nil {
				return err
			}
			ids[i] = id
		}
		var country any
		if e.country != "" {
			country = e.country
		}
		if _, err := stmt.Exec(e.ts, ids[0], e.typ, ids[1], ids[2], ids[3], ids[4], e.visitor, country, e.device, e.browser, e.os, ids[5]); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *store) intern(tx *sql.Tx, v string) (int64, error) {
	s.mu.Lock()
	id, ok := s.cache[v]
	s.mu.Unlock()
	if ok {
		return id, nil
	}
	err := tx.QueryRow(`INSERT INTO str(s) VALUES (?) ON CONFLICT(s) DO UPDATE SET s=s RETURNING id`, v).Scan(&id)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	if len(s.cache) > 50000 {
		s.cache = map[string]int64{}
	}
	s.cache[v] = id
	s.mu.Unlock()
	return id, nil
}

func (s *store) purge(before time.Time) (int64, error) {
	r, err := s.db.Exec(`DELETE FROM ev WHERE ts < ?`, before.Unix())
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}

// salt returns the visitor-hash salt for the given UTC day, creating and persisting it if needed.
// Old salts are deleted, so visitor hashes cannot be reversed or linked across days.
func (s *store) salt(day string) ([]byte, error) {
	var b []byte
	err := s.db.QueryRow(`SELECT v FROM kv WHERE k = 'salt:'||?`, day).Scan(&b)
	if err == nil {
		return b, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	b = randomBytes(32)
	if _, err := s.db.Exec(`DELETE FROM kv WHERE k LIKE 'salt:%' AND k != 'salt:'||?`, day); err != nil {
		return nil, err
	}
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO kv VALUES ('salt:'||?, ?)`, day, b); err != nil {
		return nil, err
	}
	err = s.db.QueryRow(`SELECT v FROM kv WHERE k = 'salt:'||?`, day).Scan(&b)
	return b, err
}
