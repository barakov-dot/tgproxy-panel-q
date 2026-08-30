package applier

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/barakov-dot/tgproxy-panel-q/internal/config"
)

const (
	defaultHealthCheckAttempts = 20
	defaultHealthCheckInterval = time.Second
)

func checkServiceActive(ctx context.Context, runner Runner, serviceName string) error {
	stdout, stderr, err := runner.Run(ctx, "systemctl", "is-active", serviceName)
	if err != nil {
		return fmt.Errorf("systemctl is-active: %w (stderr: %s)", err, strings.TrimSpace(stderr))
	}
	if strings.TrimSpace(stdout) != "active" {
		return fmt.Errorf("systemctl is-active %q returned %q", serviceName, strings.TrimSpace(stdout))
	}
	return nil
}

func checkReadyz(ctx context.Context, client *http.Client, baseURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/readyz", nil)
	if err != nil {
		return fmt.Errorf("build readyz request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("readyz returned status %d", resp.StatusCode)
	}
	return nil
}

// waitForHealthy polls systemctl is-active and /readyz until both succeed.
func waitForHealthy(ctx context.Context, runner Runner, client *http.Client, cfg *config.Config, attempts int, interval time.Duration) error {
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("%w: %v", ErrNotReady, ctx.Err())
			case <-time.After(interval):
			}
		}
		if err := checkServiceActive(ctx, runner, cfg.TproxyServiceName); err != nil {
			lastErr = err
			continue
		}
		if err := checkReadyz(ctx, client, cfg.TproxyAdminURL); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return fmt.Errorf("%w after %d attempts: %v", ErrNotReady, attempts, lastErr)
}
