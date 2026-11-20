package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	iphone = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"
	linux  = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"
)

func newTestStore(t *testing.T) *store {
	t.Helper()
	st, err := openStore(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.close)
	return st
}

// flush drains queued events into the DB synchronously.
func flush(t *testing.T, st *store) {
	t.Helper()
	var buf []event
	for {
		select {
		case e := <-st.in:
			buf = append(buf, e)
		default:
			if err := st.insert(buf); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
}

func post(h http.Handler, body, ua, ip, country string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/e", strings.NewReader(body))
	r.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	r.Header.Set("User-Agent", ua)
	r.Header.Set("CF-Connecting-IP", ip)
	if country != "" {
		r.Header.Set("CF-IPCountry", country)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func getStats(t *testing.T, st *store, site string) stats {
	t.Helper()
	q := url.Values{"site": {site}, "from": {"0"}, "to": {itoa(time.Now().Unix() + 10)}}
	w := httptest.NewRecorder()
	st.statsHandler(w, httptest.NewRequest(http.MethodGet, "/api/stats?"+q.Encode(), nil))
	if w.Code != 200 {
		t.Fatalf("stats: %d %s", w.Code, w.Body)
	}
	var s stats
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestIngestAndStats(t *testing.T) {
	st := newTestStore(t)
	in := &ingest{st: st}

	for _, ip := range []string{"1.1.1.1", "2.2.2.2", "2.2.2.2"} {
		if w := post(in, "s=blog&t=p&p=/a&r=https://www.Google.com/search?q=x&w=390", iphone, ip, "pl"); w.Code != 204 {
			t.Fatalf("status %d", w.Code)
		}
	}
	post(in, "s=blog&t=p&p=/b&w=1920", linux, "3.3.3.3", "XX")
	post(in, "s=blog&t=e&n=signup&p=/b&d="+url.QueryEscape("plan=pro&a=1"), linux, "3.3.3.3", "DE")
	// GET pixel fallback
	r := httptest.NewRequest(http.MethodGet, "/e?s=blog&t=p&p=/c", nil)
	r.Header.Set("User-Agent", linux)
	w := httptest.NewRecorder()
	in.ServeHTTP(w, r)
	if w.Header().Get("Content-Type") != "image/gif" || len(w.Body.Bytes()) == 0 {
		t.Fatalf("expected gif, got %q", w.Header().Get("Content-Type"))
	}
	flush(t, st)

	s := getStats(t, st, "blog")
	if s.Views != 5 || s.Events != 1 {
		t.Fatalf("views=%d events=%d", s.Views, s.Events)
	}
	if s.Visitors != 4 { // 1.1.1.1, 2.2.2.2, 3.3.3.3, and the GET from httptest RemoteAddr
		t.Fatalf("visitors=%d want 4", s.Visitors)
	}
	if s.Pages[0].K != "/a" || s.Pages[0].V != 3 || s.Pages[0].U != 2 {
		t.Fatalf("pages=%+v", s.Pages)
	}
	if s.Refs[0].K != "google.com" {
		t.Fatalf("refs=%+v", s.Refs)
	}
	if s.EvNames[0].K != "signup" || s.EvProps[0].P != "a=1&plan=pro" {
		t.Fatalf("events=%+v props=%+v", s.EvNames, s.EvProps)
	}
	if s.Country[0].K != "PL" || s.Country[0].V != 3 {
		t.Fatalf("countries=%+v", s.Country)
	}
	if s.Devices[0].K != "Mobile" || s.OS[0].K != "iOS" || s.Browsers[0].K != "Safari" {
		t.Fatalf("devices=%+v os=%+v browsers=%+v", s.Devices, s.OS, s.Browsers)
	}
	if len(s.Series) == 0 || s.Bucket != "day" {
		t.Fatalf("series=%+v bucket=%s", s.Series, s.Bucket)
	}
	if got := getStats(t, st, "nope"); got.Views != 0 {
		t.Fatal("unknown site should be empty")
	}
}

func TestIngestRejects(t *testing.T) {
	st := newTestStore(t)
	in := &ingest{st: st, sites: map[string]bool{"blog": true}}
	cases := map[string]struct{ body, ua string }{
		"bot":           {"s=blog&p=/", "Googlebot/2.1"},
		"no ua":         {"s=blog&p=/", ""},
		"other site":    {"s=evil&p=/", linux},
		"no site":       {"p=/", linux},
		"no path":       {"s=blog&t=p", linux},
		"event no name": {"s=blog&t=e&p=/", linux},
		"bad type":      {"s=blog&t=x&p=/", linux},
	}
	for name, c := range cases {
		if w := post(in, c.body, c.ua, "1.1.1.1", ""); w.Code != 204 {
			t.Errorf("%s: status %d", name, w.Code)
		}
	}
	if len(st.in) != 0 {
		t.Fatalf("expected nothing queued, got %d", len(st.in))
	}
	r := httptest.NewRequest(http.MethodPost, "/e", strings.NewReader("s=blog&p=/"))
	r.Header.Set("User-Agent", linux)
	r.Header.Set("Sec-Purpose", "prefetch")
	in.ServeHTTP(httptest.NewRecorder(), r)
	if len(st.in) != 0 {
		t.Fatal("prefetch should be ignored")
	}
}

func TestVisitorHashRotatesAndIsStable(t *testing.T) {
	st := newTestStore(t)
	in := &ingest{st: st}
	a, _ := in.visitor("1.1.1.1", linux, "blog")
	b, _ := in.visitor("1.1.1.1", linux, "blog")
	c, _ := in.visitor("1.1.1.1", linux, "other")
	if a != b || a == c {
		t.Fatal("hash must be stable per site and differ across sites")
	}
	in.saltDay = "" // force reload from DB: same day => same salt
	if d, _ := in.visitor("1.1.1.1", linux, "blog"); d != a {
		t.Fatal("salt must persist across restarts within a day")
	}
	s1, _ := st.salt("2000-01-01")
	s2, _ := st.salt("2000-01-02")
	if string(s1) == string(s2) {
		t.Fatal("salt must change per day")
	}
	var n int
	st.db.QueryRow(`SELECT count(*) FROM kv WHERE k LIKE 'salt:%'`).Scan(&n)
	if n != 1 {
		t.Fatalf("old salts must be deleted, have %d", n)
	}
}

func TestPurge(t *testing.T) {
	st := newTestStore(t)
	old := time.Now().AddDate(0, 0, -10).Unix()
	if err := st.insert([]event{{ts: old, site: "s", path: "/"}, {ts: time.Now().Unix(), site: "s", path: "/"}}); err != nil {
		t.Fatal(err)
	}
	n, err := st.purge(time.Now().AddDate(0, 0, -5))
	if err != nil || n != 1 {
		t.Fatalf("purged %d err %v", n, err)
	}
}

func TestAuth(t *testing.T) {
	h := auth(config{user: "admin", pass: "pw"}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	for _, c := range []struct {
		u, p string
		want int
	}{{"admin", "pw", 200}, {"admin", "x", 401}, {"", "", 401}} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if c.u != "" {
			r.SetBasicAuth(c.u, c.p)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("%s:%s => %d want %d", c.u, c.p, w.Code, c.want)
		}
	}
	// no password configured => always locked
	h = auth(config{user: "admin"}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.SetBasicAuth("admin", "")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("empty password must disable dashboard")
	}
}

func TestWriterFlushesOnShutdown(t *testing.T) {
	st := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { st.writer(ctx); close(done) }()
	st.push(event{ts: time.Now().Unix(), site: "s", path: "/"})
	cancel()
	<-done
	var n int
	st.db.QueryRow(`SELECT count(*) FROM ev`).Scan(&n)
	if n != 1 {
		t.Fatalf("expected 1 row after shutdown flush, got %d", n)
	}
}

func TestParseUA(t *testing.T) {
	cases := []struct {
		ua      string
		w       int
		b, o, d string
	}{
		{iphone, 0, "Safari", "iOS", "Mobile"},
		{linux, 1920, "Chrome", "Linux", "Desktop"},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/130 Safari/537.36 Edg/130", 0, "Edge", "Windows", "Desktop"},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0; rv:131.0) Gecko/20100101 Firefox/131.0", 0, "Firefox", "macOS", "Desktop"},
		{"Mozilla/5.0 (Linux; Android 14; SM-X710) AppleWebKit/537.36 Chrome/130 Safari/537.36", 0, "Chrome", "Android", "Tablet"},
		{"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 Chrome/130 Mobile Safari/537.36", 0, "Chrome", "Android", "Mobile"},
		{linux, 400, "Chrome", "Linux", "Mobile"},
	}
	for _, c := range cases {
		b, o, d := parseUA(c.ua, c.w)
		if browsers[b] != c.b || oses[o] != c.o || devices[d] != c.d {
			t.Errorf("%s => %s/%s/%s want %s/%s/%s", c.ua, browsers[b], oses[o], devices[d], c.b, c.o, c.d)
		}
	}
	for _, ua := range []string{"Googlebot/2.1", "curl/8.0", "Mozilla/5.0 HeadlessChrome/130", ""} {
		if !isBot(ua) {
			t.Errorf("isBot(%q) = false", ua)
		}
	}
	if isBot(linux) {
		t.Error("real browser flagged as bot")
	}
}

func TestHelpers(t *testing.T) {
	for in, want := range map[string]string{
		"https://www.Example.com/x?y": "example.com",
		"newsletter":                  "newsletter",
		"":                            "",
	} {
		if got := refHost(in); got != want {
			t.Errorf("refHost(%q)=%q want %q", in, got, want)
		}
	}
	if normProps("b=2&a=1") != "a=1&b=2" || normProps("") != "" {
		t.Error("normProps must sort keys")
	}
	if clip("  abcdef ", 3) != "abc" {
		t.Error("clip")
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "9.9.9.9:1234"
	r.Header.Set("X-Forwarded-For", "8.8.8.8, 10.0.0.1")
	if clientIP(r) != "8.8.8.8" {
		t.Error("xff")
	}
	r.Header.Set("CF-Connecting-IP", "7.7.7.7")
	if clientIP(r) != "7.7.7.7" {
		t.Error("cf ip should win")
	}
}

// Verifies the SigV4 signer against a local fake R2 that recomputes the signature.
func TestR2PutSigns(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r }))
	defer srv.Close()
	c := &r2Config{account: "acc", bucket: "b", key: "AK", secret: "SK"}
	// Point the client at the test server by swapping the transport.
	old := http.DefaultClient.Transport
	http.DefaultClient.Transport = rewrite{srv.URL}
	defer func() { http.DefaultClient.Transport = old }()
	if err := r2Put(context.Background(), c, "x/y.gz", strings.NewReader("hello"), 5); err != nil {
		t.Fatal(err)
	}
	a := got.Header.Get("Authorization")
	if got.Method != "PUT" || got.URL.Path != "/b/x/y.gz" || !strings.HasPrefix(a, "AWS4-HMAC-SHA256 Credential=AK/") || !strings.Contains(a, "Signature=") {
		t.Fatalf("bad request: %s %s %s", got.Method, got.URL.Path, a)
	}
}

type rewrite struct{ to string }

func (r rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	u, _ := url.Parse(r.to)
	req.URL.Scheme, req.URL.Host = u.Scheme, u.Host
	return http.DefaultTransport.RoundTrip(req)
}
