package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chwiee/k8s-mcp/internal/app"
)

func main() {
	cfg := app.LoadConfig()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	kbStore, err := app.NewKBStore(cfg.KBDir)
	if err != nil {
		logger.Error("failed to load knowledge base", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	kbStore.Start(ctx, cfg.KBRefreshInterval, logger)

	inspector, err := app.NewKubeInspector(cfg.Kubeconfig)
	if err != nil {
		logger.Warn("kubernetes client is unavailable, running in knowledge-base-only mode", "error", err)
		inspector = app.NewUnavailableInspector(err)
	}

	server := app.NewServer(cfg, kbStore, inspector, logger)
	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           server.Handler(),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	logger.Info("starting k8s-mcp server", "port", cfg.Port, "kb_dir", cfg.KBDir)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}
