// Package notify holds outbound notification helpers (signed webhooks).
package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// PostSignedJSON sends payload as JSON with HMAC-SHA256 signature headers.
// Signature covers timestamp + "." + raw body bytes (same pattern as Stripe/GitHub webhooks).
func PostSignedJSON(ctx context.Context, cli *http.Client, endpointURL string, secret []byte, payload []byte) error {
	if cli == nil {
		cli = http.DefaultClient
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(ts + "."))
	mac.Write(payload)
	sig := hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Sonar-Timestamp", ts)
	req.Header.Set("X-Sonar-Signature", "sha256="+sig)

	resp, err := cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook POST %s: %s", resp.Status, string(body))
	}
	return nil
}
