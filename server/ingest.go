package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 1x1 transparent GIF, returned for GET (no-JS / <img>) requests.
var gif = []byte("GIF89a\x01\x00\x01\x00\x80\x00\x00\x00\x00\x00\x00\x00\x00!\xf9\x04\x01\x00\x00\x00\x00,\x00\x00\x00\x00\x01\x00\x01\x00\x00\x02\x02D\x01\x00;")

type ingest struct {
	st    *store
	sites map[string]bool

	mu      sync.Mutex
	saltDay string
	saltVal []byte
}

// ServeHTTP accepts events as url-encoded key/values, either as a POST body
// (navigator.sendBeacon, text/plain => no CORS preflight) or as a GET query string.
//
// Fields: s=site t=p|e p=path n=name r=referrer w=screen width d=props (url-encoded k=v pairs)
func (in *ingest) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	var q url.Values
	switch r.Method {
	case http.MethodPost:
		b, err := io.ReadAll(io.LimitReader(r.Body, 4096))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		q, _ = url.ParseQuery(string(b))
	case http.MethodGet:
		q = r.URL.Query()
	case http.MethodOptions:
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	defer func() {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "image/gif")
			w.Header().Set("Cache-Control", "no-store")
			w.Write(gif)
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
	}()

	ua := r.UserAgent()
	if isBot(ua) || r.Header.Get("Purpose") == "prefetch" || r.Header.Get("Sec-Purpose") != "" {
		return
	}
	site := clip(q.Get("s"), 64)
	if site == "" || (in.sites != nil && !in.sites[site]) {
		return
	}
	e := event{ts: time.Now().Unix(), site: site, path: clip(q.Get("p"), 512)}
	switch q.Get("t") {
	case "p", "":
		if e.path == "" {
			return
		}
		e.ref = refHost(q.Get("r"))
	case "e":
		e.typ = 1
		e.name = clip(q.Get("n"), 128)
		if e.name == "" {
			return
		}
		e.props = normProps(q.Get("d"))
	default:
		return
	}
	e.browser, e.os, e.device = parseUA(ua, atoiDef(q.Get("w"), 0))
	e.country = strings.ToUpper(clip(r.Header.Get("CF-IPCountry"), 2))
	if e.country == "XX" || e.country == "T1" {
		e.country = ""
	}
	v, err := in.visitor(clientIP(r), ua, site)
	if err != nil {
		log.Println("salt:", err)
		return
	}
	e.visitor = v
	in.st.push(e)
}

// visitor derives an anonymous daily visitor id: hash(daily salt, ip, ua, site).
// The raw IP is never stored, and the salt rotates daily.
func (in *ingest) visitor(ip, ua, site string) (int64, error) {
	day := time.Now().UTC().Format("2006-01-02")
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.saltDay != day {
		s, err := in.st.salt(day)
		if err != nil {
			return 0, err
		}
		in.saltDay, in.saltVal = day, s
	}
	h := sha256.New()
	h.Write(in.saltVal)
	io.WriteString(h, ip+"\x00"+ua+"\x00"+site)
	return int64(binary.BigEndian.Uint64(h.Sum(nil)[:8])), nil
}

func clientIP(r *http.Request) string {
	if ip := r.Header.Get("CF-Connecting-IP"); ip != "" {
		return ip
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

// refHost reduces a referrer to its host (or keeps a plain utm source token).
func refHost(r string) string {
	if r == "" {
		return ""
	}
	if u, err := url.Parse(r); err == nil && u.Host != "" {
		return clip(strings.TrimPrefix(strings.ToLower(u.Hostname()), "www."), 128)
	}
	return clip(strings.ToLower(r), 128)
}

// normProps re-encodes props deterministically (sorted keys) and caps size,
// so identical props intern to the same string.
func normProps(d string) string {
	if d == "" {
		return ""
	}
	v, err := url.ParseQuery(d)
	if err != nil || len(v) == 0 {
		return ""
	}
	return clip(v.Encode(), 512)
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}

func atoiDef(s string, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

func secureEq(a, b string) bool {
	x, y := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(x[:], y[:]) == 1
}
