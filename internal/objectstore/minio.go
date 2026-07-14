// Package objectstore stores large document bodies in MinIO/S3-compatible storage.
package objectstore

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Config holds MinIO connection settings from SONAR_MINIO_* env vars.
type Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

// LoadConfigFromEnv reads SONAR_MINIO_ENDPOINT, USER, PASSWORD.
func LoadConfigFromEnv() (Config, bool) {
	ep := strings.TrimSpace(os.Getenv("SONAR_MINIO_ENDPOINT"))
	user := strings.TrimSpace(os.Getenv("SONAR_MINIO_USER"))
	pass := strings.TrimSpace(os.Getenv("SONAR_MINIO_PASSWORD"))
	if ep == "" || user == "" || pass == "" {
		return Config{}, false
	}
	bucket := strings.TrimSpace(os.Getenv("SONAR_MINIO_BUCKET"))
	if bucket == "" {
		bucket = "sonar-documents"
	}
	useSSL := strings.EqualFold(os.Getenv("SONAR_MINIO_SSL"), "true")
	return Config{Endpoint: ep, AccessKey: user, SecretKey: pass, Bucket: bucket, UseSSL: useSSL}, true
}

// Client uploads and downloads objects via S3-compatible PUT/GET.
type Client struct {
	cfg Config
	http *http.Client
}

// New returns a client when config is valid.
func New(cfg Config) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: 120 * time.Second}}
}

// Put stores body at key and returns bucket+key for DB persistence.
func (c *Client) Put(ctx context.Context, key string, body []byte, contentType string) (bucket, objectKey string, err error) {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	scheme := "http"
	if c.cfg.UseSSL {
		scheme = "https"
	}
	url := fmt.Sprintf("%s://%s/%s/%s", scheme, strings.TrimPrefix(c.cfg.Endpoint, "/"), c.cfg.Bucket, key)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Content-Length", fmt.Sprintf("%d", len(body)))
	c.sign(req, body)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", "", fmt.Errorf("objectstore put: %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return c.cfg.Bucket, key, nil
}

// Get downloads an object by bucket/key.
func (c *Client) Get(ctx context.Context, bucket, key string) ([]byte, error) {
	scheme := "http"
	if c.cfg.UseSSL {
		scheme = "https"
	}
	if bucket == "" {
		bucket = c.cfg.Bucket
	}
	url := fmt.Sprintf("%s://%s/%s/%s", scheme, strings.TrimPrefix(c.cfg.Endpoint, "/"), bucket, key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	c.sign(req, nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("objectstore get: %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return io.ReadAll(resp.Body)
}

// sign adds a minimal AWS SigV4 Authorization header for MinIO.
func (c *Client) sign(req *http.Request, body []byte) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	req.Header.Set("X-Amz-Date", amzDate)
	payloadHash := sha256Hex(body)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	host := req.URL.Host
	canonicalURI := req.URL.EscapedPath()
	canonicalHeaders := fmt.Sprintf("host:%s\nx-amz-content-sha256:%s\nx-amz-date:%s\n", host, payloadHash, amzDate)
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalRequest := strings.Join([]string{
		req.Method, canonicalURI, "", canonicalHeaders, signedHeaders, payloadHash,
	}, "\n")
	credentialScope := dateStamp + "/us-east-1/s3/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256", amzDate, credentialScope, sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	signingKey := deriveSigningKey(c.cfg.SecretKey, dateStamp, "us-east-1", "s3")
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))
	auth := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		c.cfg.AccessKey, credentialScope, signedHeaders, signature)
	req.Header.Set("Authorization", auth)
}

func sha256Hex(b []byte) string {
	if b == nil {
		b = []byte{}
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key, msg []byte) []byte {
	m := hmac.New(sha256.New, key)
	_, _ = m.Write(msg)
	return m.Sum(nil)
}

func deriveSigningKey(secret, dateStamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	return hmacSHA256(kService, []byte("aws4_request"))
}
