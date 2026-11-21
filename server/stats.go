package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type row struct {
	K string `json:"k"`           // label
	V int64  `json:"v"`           // count (views or events)
	U int64  `json:"u"`           // unique visitors
	P string `json:"p,omitempty"` // props (event breakdown only)
}

type point struct {
	T int64 `json:"t"` // bucket start, unix seconds (UTC)
	V int64 `json:"v"`
	U int64 `json:"u"`
}

type stats struct {
	Bucket   string  `json:"bucket"` // "hour" | "day"
	Views    int64   `json:"views"`
	Visitors int64   `json:"visitors"`
	Events   int64   `json:"events"`
	Series   []point `json:"series"`
	Pages    []row   `json:"pages"`
	Refs     []row   `json:"refs"`
	EvNames  []row   `json:"eventNames"`
	EvProps  []row   `json:"eventProps"`
	Country  []row   `json:"countries"`
	Devices  []row   `json:"devices"`
	Browsers []row   `json:"browsers"`
	OS       []row   `json:"os"`
	Versions []row   `json:"versions"`
}

// statsHandler: GET /api/stats?site=blog&from=<unix>&to=<unix>&tz=<minutes, as JS getTimezoneOffset()>
func (s *store) statsHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	now := time.Now().Unix()
	to := int64(atoiDef(q.Get("to"), int(now)))
	from := int64(atoiDef(q.Get("from"), int(to-7*86400)))
	tz := int64(atoiDef(q.Get("tz"), 0)) * 60 // seconds to add to local to get UTC

	var site int64
	if err := s.db.QueryRow(`SELECT id FROM str WHERE s = ?`, q.Get("site")).Scan(&site); err != nil {
		writeJSON(w, stats{Bucket: "day"})
		return
	}

	out := stats{Bucket: "day"}
	size := int64(86400)
	if to-from <= 3*86400 {
		out.Bucket, size = "hour", 3600
	}
	where := ` FROM ev WHERE site = ? AND ts >= ? AND ts < ?`
	args := []any{site, from, to}

	var err error
	must := func(e error) {
		if err == nil && e != nil {
			err = e
		}
	}
	must(s.db.QueryRow(`SELECT count(*) FILTER (WHERE typ=0), count(DISTINCT visitor), count(*) FILTER (WHERE typ=1)`+where, args...).
		Scan(&out.Views, &out.Visitors, &out.Events))

	// Buckets are aligned to the viewer's local midnight/hour: local = utc - tz.
	pts, e := queryPoints(s.db, `SELECT ((ts - ?) / ?) * ? + ? AS b, count(*), count(DISTINCT visitor)`+where+` AND typ=0 GROUP BY b ORDER BY b`,
		append([]any{tz, size, size, tz}, args...)...)
	must(e)
	out.Series = pts

	top := func(sel, group string, typ int, labels []string, cond ...string) []row {
		extra := strings.Join(append([]string{""}, cond...), " AND ")
		rows, e := queryRows(s.db, `SELECT `+sel+`, count(*), count(DISTINCT visitor)`+where+` AND typ=`+strconv.Itoa(typ)+extra+` GROUP BY `+group+` ORDER BY 2 DESC LIMIT 50`, labels, args...)
		must(e)
		return rows
	}
	strOf := func(col string) string { return `(SELECT s FROM str WHERE id = ev.` + col + `)` }

	out.Pages = top(strOf("path"), "path", 0, nil)
	out.Refs = top(strOf("ref"), "ref", 0, nil)
	out.EvNames = top(strOf("name"), "name", 1, nil)
	out.Country = top("country", "country", 0, nil)
	out.Devices = top("device", "device", 0, devices)
	out.Browsers = top("browser", "browser", 0, browsers)
	out.OS = top("os", "os", 0, oses)
	// Only clients that report a version (mostly desktop apps); unversioned traffic would just be one big blank row.
	out.Versions = top(strOf("ver"), "ver", 0, nil, "ver IS NOT NULL")

	rows, e := s.db.Query(`SELECT `+strOf("name")+`, coalesce(`+strOf("props")+`, ''), count(*), count(DISTINCT visitor)`+where+` AND typ=1 AND props IS NOT NULL GROUP BY name, props ORDER BY 3 DESC LIMIT 100`, args...)
	must(e)
	if e == nil {
		for rows.Next() {
			var x row
			rows.Scan(&x.K, &x.P, &x.V, &x.U)
			out.EvProps = append(out.EvProps, x)
		}
		rows.Close()
	}

	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, out)
}

func (s *store) sitesHandler(w http.ResponseWriter, _ *http.Request) {
	rows, err := s.db.Query(`SELECT s FROM str WHERE id IN (SELECT DISTINCT site FROM ev) ORDER BY s`)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()
	sites := []string{}
	for rows.Next() {
		var x string
		rows.Scan(&x)
		sites = append(sites, x)
	}
	writeJSON(w, sites)
}

func queryPoints(db *sql.DB, q string, args ...any) ([]point, error) {
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []point{}
	for rows.Next() {
		var p point
		if err := rows.Scan(&p.T, &p.V, &p.U); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// queryRows scans (label, count, uniques). If labels is set, the label column is an enum index.
func queryRows(db *sql.DB, q string, labels []string, args ...any) ([]row, error) {
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []row{}
	for rows.Next() {
		var k sql.NullString
		var x row
		if err := rows.Scan(&k, &x.V, &x.U); err != nil {
			return nil, err
		}
		x.K = k.String
		if labels != nil {
			if i, _ := strconv.Atoi(k.String); i >= 0 && i < len(labels) {
				x.K = labels[i]
			}
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}
