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

	"feed/internal/config"
	"feed/internal/handler"
	"feed/internal/svc"
)

func main() {
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config failed", "error", err)
		os.Exit(1)
	}

	serviceContext, err := svc.NewServiceContext(rootCtx, cfg, logger)
	if err != nil {
		logger.Error("initialize service context failed", "error", err)
		os.Exit(1)
	}
	defer serviceContext.Close()

	if err := serviceContext.Start(rootCtx); err != nil {
		logger.Error("start background workers failed", "error", err)
		os.Exit(1)
	}

	server := &http.Server{
		Addr:              cfg.App.HTTPAddr,
		Handler:           handler.NewRouter(serviceContext, logger),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-rootCtx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("http server shutdown failed", "error", err)
		}
	}()

	logger.Info("server started", "addr", cfg.App.HTTPAddr, "app", cfg.App.Name)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("http server stopped unexpectedly", "error", err)
		os.Exit(1)
	}

	logger.Info("server stopped")
}
