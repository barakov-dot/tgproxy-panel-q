// Command tgproxy-panel runs the admin web panel and Telegram bot that issue
// and revoke tproxy-server proxy profiles, in one process, sharing one
// SQLite store and one internal/applier instance.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/barakov-dot/tgproxy-panel/internal/applier"
	"github.com/barakov-dot/tgproxy-panel/internal/auth"
	"github.com/barakov-dot/tgproxy-panel/internal/bot"
	"github.com/barakov-dot/tgproxy-panel/internal/config"
	"github.com/barakov-dot/tgproxy-panel/internal/httpserver"
	"github.com/barakov-dot/tgproxy-panel/internal/logging"
	"github.com/barakov-dot/tgproxy-panel/internal/models"
	"github.com/barakov-dot/tgproxy-panel/internal/store"
)

// shutdownTimeout bounds how long we wait for in-flight HTTP requests to
// finish on SIGINT/SIGTERM before forcing the listener closed. It does not
// bound the bot: a request in flight there (issue/revoke) may be mid
// systemctl-restart-and-poll-/readyz, which legitimately takes tens of
// seconds (see internal/applier) — the bot goroutine is given the same
// grace via ctx cancellation and is simply awaited, not force-timed-out.
const shutdownTimeout = 10 * time.Second

func main() {
	if err := run(); err != nil {
		slog.Error("tgproxy-panel: fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	log := logging.New(cfg.LogFormat)
	slog.SetDefault(log)

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	if err := seedAutoIssueSetting(context.Background(), st, cfg.AutoIssue); err != nil {
		return fmt.Errorf("seed auto_issue setting: %w", err)
	}

	ap := applier.New(cfg, st)

	sessions := auth.NewSessions(cfg.SessionSecret)
	limiter := auth.NewDefaultLoginLimiter()

	srv, err := httpserver.New(cfg, st, ap, sessions, limiter, log)
	if err != nil {
		return fmt.Errorf("build http server: %w", err)
	}

	httpSrv := &http.Server{
		Addr:    "127.0.0.1:" + strconv.Itoa(cfg.PanelPort),
		Handler: srv.Handler(),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup
	var botErr error

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Info("bot: starting long polling")
		if err := bot.Run(ctx, cfg, st, ap); err != nil {
			botErr = err
			log.Error("bot: stopped with error", "error", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Info("http: listening", "addr", httpSrv.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http: stopped with error", "error", err)
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Error("http: graceful shutdown failed", "error", err)
	}

	wg.Wait()
	return botErr
}

// seedAutoIssueSetting writes the settings table's auto_issue row from
// Config.AutoIssue the first time the panel ever starts (plan.md §9:
// AUTO_ISSUE in .env is only the initial default — after that, the panel's
// settings page is authoritative and this must not overwrite an admin's
// later choice on every restart).
func seedAutoIssueSetting(ctx context.Context, st *store.Store, defaultAutoIssue bool) error {
	_, ok, err := st.GetSetting(ctx, models.SettingAutoIssue)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	return st.SetSetting(ctx, models.SettingAutoIssue, strconv.FormatBool(defaultAutoIssue))
}
