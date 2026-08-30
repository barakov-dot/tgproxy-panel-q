package applier

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Reference install.sh polls /readyz 20 times with a 1s sleep between
// attempts after restarting tproxy-server; we match that shape here so
// behavior stays consistent with what the upstream installer already
// proved works, rather than inventing new timing.
const (
	defaultHealthCheckAttempts = 20
	defaultHealthCheckInterval = time.Second
)

// waitForReady polls baseURL+"/readyz" until it returns 200, up to
// attempts times with interval between tries. It returns ErrNotReady
// (wrapping the last failure) if the service never became ready.
func waitForReady(ctx context.Context, client *http.Client, baseURL string, attempts int, interval time.Duration) error {
	url := baseURL + "/readyz"

	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("%w: %v", ErrNotReady, ctx.Err())
			case <-time.After(interval):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("applier: build readyz request: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return nil
		}
		lastErr = fmt.Errorf("readyz returned status %d", resp.StatusCode)
	}
	return fmt.Errorf("%w after %d attempts: %v", ErrNotReady, attempts, lastErr)
}
