// Command kabanbot runs the Telegram bot.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/feytox/kabanbot/internal/app/ingest"
	"github.com/feytox/kabanbot/internal/app/mention"
	"github.com/feytox/kabanbot/internal/app/settings"
	"github.com/feytox/kabanbot/internal/app/summary"
	"github.com/feytox/kabanbot/internal/config"
	"github.com/feytox/kabanbot/internal/domain"
	"github.com/feytox/kabanbot/internal/llm"
	"github.com/feytox/kabanbot/internal/llm/registry"
	"github.com/feytox/kabanbot/internal/secrets"
	"github.com/feytox/kabanbot/internal/storage/sqlite"
	"github.com/feytox/kabanbot/internal/telegram"
	"github.com/feytox/kabanbot/prompts"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx)
	stop()
	if err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

	db, err := sqlite.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	var box *secrets.Box
	if key, _ := cfg.MasterKeyBytes(); key != nil {
		if box, err = secrets.New(key); err != nil {
			return err
		}
	} else {
		log.Warn("MASTER_KEY is not set: users cannot connect their own models")
	}

	fallback, err := defaultTarget(ctx, cfg.LLM)
	if err != nil {
		return err
	}
	modelStore := newModelStore(db, box)
	models := registry.New(modelStore, fallback, cfg.OwnerID)
	messages := sqlite.NewMessageStore(db)
	chats := sqlite.NewChatStore(db)

	tg, err := telegram.Connect(ctx, cfg.BotToken)
	if err != nil {
		return err
	}
	bot := telegram.NewBot(tg, telegram.Deps{
		Ingest:   ingest.New(messages, cfg.CacheSize),
		Summary:  summary.New(messages, models, prompts.Summary()),
		Mention:  mention.New(messages, tg, log),
		Chats:    chats,
		Settings: settings.New(modelStore, chats, tg, models, log),
		Allowed:  cfg.IsAllowed,
	}, log)

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error { return bot.Run(ctx) })
	g.Go(func() error { return serveHealth(ctx, cfg.HTTPAddr, db) })
	return g.Wait()
}

// newModelStore avoids passing a typed nil *secrets.Box as a non-nil interface.
func newModelStore(db *sqlite.DB, box *secrets.Box) *sqlite.ModelStore {
	if box == nil {
		return sqlite.NewModelStore(db, nil)
	}
	return sqlite.NewModelStore(db, box)
}

func defaultTarget(ctx context.Context, c config.DefaultLLM) (llm.Target, error) {
	// The default model is configured by the operator, so it may point at a local server.
	client, err := registry.NewClient(ctx, domain.Provider{
		Kind:    c.Provider,
		BaseURL: c.BaseURL,
		APIKey:  domain.Secret(c.APIKey),
	}, nil)
	if err != nil {
		return llm.Target{}, fmt.Errorf("default llm: %w", err)
	}
	return llm.Target{Client: client, Model: domain.Model{Name: c.Model, DisplayName: c.Model}}, nil
}

// serveHealth serves /healthz for container health checks.
func serveHealth(ctx context.Context, addr string, db *sqlite.DB) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(r.Context()); err != nil {
			http.Error(w, "db unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http: %w", err)
	}
	return nil
}
