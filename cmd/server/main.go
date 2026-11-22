package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/officiallysidsingh/go-notify/config"
	"github.com/officiallysidsingh/go-notify/internal/app"
	"github.com/officiallysidsingh/go-notify/internal/logging"
)

func main() {
	cfg, err := config.Load("./config")
	exitOn(err)
	exitOn(cfg.ValidateServer())

	logger, err := logging.New(cfg.Logging.Level)
	exitOn(err)
	defer func() { _ = logger.Sync() }()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := app.RunServer(ctx, cfg, logger); err != nil {
		logger.Error("server exited with error", zap.Error(err))
		stop()
		_ = logger.Sync()
		os.Exit(1)
	}
}

// exitOn reports a startup error on stderr (the logger is not up yet) and exits.
func exitOn(err error) {
	if err != nil {
		_, _ = io.WriteString(os.Stderr, err.Error()+"\n")
		os.Exit(1)
	}
}
