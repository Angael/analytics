package main

import (
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// r2Config holds Cloudflare R2 (S3-compatible) credentials for daily backups.
type r2Config struct {
	account, bucket, key, secret, prefix string
}

func loadR2() *r2Config {
	c := &r2Config{
		account: os.Getenv("R2_ACCOUNT_ID"),
		bucket:  os.Getenv("R2_BUCKET"),
		key:     os.Getenv("R2_ACCESS_KEY_ID"),
		secret:  os.Getenv("R2_SECRET_ACCESS_KEY"),
		prefix:  env("R2_PREFIX", "analytics/"),
	}
	if c.account == "" || c.bucket == "" || c.key == "" || c.secret == "" {
		return nil
	}
	return c
}

// backup snapshots the DB (VACUUM INTO = consistent, compacted copy), gzips it and uploads it to R2.
// Use an R2 lifecycle rule to expire old backups.
func backup(ctx context.Context, st *store, c *r2Config, day string) error {
	dir := filepath.Dir(dbPathOf(st))
	snap := filepath.Join(dir, ".backup.db")
	gz := snap + ".gz"
	defer os.Remove(snap)
	defer os.Remove(gz)
	os.Remove(snap)
	if _, err := st.db.ExecContext(ctx, `VACUUM INTO ?`, snap); err != nil {
		return err
	}
	if err := gzipFile(snap, gz); err != nil {
		return err
	}
	f, err := os.Open(gz)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, _ := f.Stat()
	return r2Put(ctx, c, c.prefix+"analytics-"+day+".db.gz", f, fi.Size())
}

func dbPathOf(*store) string { return env("DB_PATH", "/data/analytics.db") }

func gzipFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	zw, _ := gzip.NewWriterLevel(out, gzip.BestCompression)
	if _, err := io.Copy(zw, in); err != nil {
		out.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// r2Put does a single SigV4-signed S3 PutObject. No SDK, ~40 lines instead of ~20MB of deps.
func r2Put(ctx context.Context, c *r2Config, key string, body io.Reader, size int64) error {
	host := c.account + ".r2.cloudflarestorage.com"
	path := "/" + c.bucket + "/" + key
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	date := now.Format("20060102")
	const region, service, payload = "auto", "s3", "UNSIGNED-PAYLOAD"

	canonical := "PUT\n" + path + "\n\n" +
		"host:" + host + "\nx-amz-content-sha256:" + payload + "\nx-amz-date:" + amzDate + "\n\n" +
		"host;x-amz-content-sha256;x-amz-date\n" + payload
	scope := date + "/" + region + "/" + service + "/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sha256hex([]byte(canonical))
	k := hmacSHA([]byte("AWS4"+c.secret), date)
	k = hmacSHA(k, region)
	k = hmacSHA(k, service)
	k = hmacSHA(k, "aws4_request")
	sig := hex.EncodeToString(hmacSHA(k, toSign))

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, "https://"+host+path, body)
	if err != nil {
		return err
	}
	req.ContentLength = size
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payload)
	req.Header.Set("Authorization", fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=host;x-amz-content-sha256;x-amz-date, Signature=%s", c.key, scope, sig))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("r2 put %s: %s %s", key, res.Status, b)
	}
	return nil
}

func hmacSHA(k []byte, s string) []byte {
	h := hmac.New(sha256.New, k)
	h.Write([]byte(s))
	return h.Sum(nil)
}

func sha256hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
