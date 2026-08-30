// Command tgproxy-panel runs the admin web panel and Telegram bot that issue
// and revoke tproxy-server proxy profiles, in one process, sharing one
// SQLite store and one internal/applier instance.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/barakov-dot/tgproxy-panel-q/internal/applier"
	"github.com/barakov-dot/tgproxy-panel-q/internal/auth"
	"github.com/barakov-dot/tgproxy-panel-q/internal/bot"
	"github.com/barakov-dot/tgproxy-panel-q/internal/config"
	"github.com/barakov-dot/tgproxy-panel-q/internal/httpserver"
	"github.com/barakov-dot/tgproxy-panel-q/internal/logging"
	"github.com/barakov-dot/tgproxy-panel-q/internal/models"
	"github.com/barakov-dot/tgproxy-panel-q/internal/service"
	"github.com/barakov-dot/tgproxy-panel-q/internal/store"
	"golang.org/x/term"
)

const shutdownTimeout = 10 * time.Second

func main() {
	hashPassword := flag.Bool("hash-password", false, "read a password (from stdin, or interactively with echo off if stdin is a terminal), print its bcrypt hash to stdout, and exit")
	flag.Parse()

	if *hashPassword {
		if err := runHashPassword(os.Stdin, os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, "tgproxy-panel: -hash-password:", err)
			os.Exit(1)
		}
		return
	}

	if err := run(); err != nil {
		slog.Error("tgproxy-panel: fatal", "error", err)
		os.Exit(1)
	}
}

func runHashPassword(in *os.File, out io.Writer, errOut io.Writer) error {
	password, err := readPassword(in, errOut)
	if err != nil {
		return err
	}
	hash, err := config.HashPassword(password)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, hash)
	return nil
}

func readPassword(in *os.File, errOut io.Writer) (string, error) {
	if term.IsTerminal(int(in.Fd())) {
		fmt.Fprint(errOut, "Password: ")
		b, err := term.ReadPassword(int(in.Fd()))
		fmt.Fprintln(errOut)
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}
		return string(b), nil
	}
	return readLine(in)
}

func readLine(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("read from stdin: %w", err)
		}
		return "", errors.New("no input on stdin")
	}
	return strings.TrimRight(scanner.Text(), "\r\n"), nil
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	log := logging.New(cfg.LogFormat)
	logging.SetDefault(log)

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	if err := seedAutoIssueSetting(context.Background(), st, cfg.AutoIssue); err != nil {
		return fmt.Errorf("seed auto_issue setting: %w", err)
	}

	ap := applier.New(cfg, applier.Store{Store: st})

	var botRef botSenderRef
	svc := service.New(cfg, st, ap, &botRef)

	panelBot, err := bot.New(cfg, svc)
	if err != nil {
		return fmt.Errorf("init bot: %w", err)
	}
	botRef.b = panelBot

	sessions := auth.NewSessions(cfg.SessionSecret)
	limiter := auth.NewDefaultLoginLimiter()

	srv, err := httpserver.New(cfg, svc, sessions, limiter, log)
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
		if err := panelBot.Run(ctx); err != nil {
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

// botSenderRef breaks the bot/service init cycle: service needs a BotSender
// at construction time, but bot.New needs the service.
type botSenderRef struct {
	b *bot.Bot
}

func (r *botSenderRef) SendProxyLink(ctx context.Context, telegramID int64, link string) error {
	if r.b == nil {
		return service.ErrNoBotSender
	}
	return r.b.SendProxyLink(ctx, telegramID, link)
}
