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
	os.Exit(run())
}

// run returns the process exit code so deferred cleanup runs before os.Exit.
func run() int {
	cfg, err := config.Load("./config")
	exitOn(err)
	exitOn(cfg.ValidateWorker())

	logger, err := logging.New(cfg.Logging.Level)
	exitOn(err)
	defer func() { _ = logger.Sync() }()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := app.RunWorker(ctx, cfg, logger); err != nil {
		logger.Error("worker exited with error", zap.Error(err))
		stop()
		_ = logger.Sync()
		return 1
	}
	return 0
}

// exitOn reports a startup error on stderr (the logger is not up yet) and exits.
func exitOn(err error) {
	if err != nil {
		_, _ = io.WriteString(os.Stderr, err.Error()+"\n")
		os.Exit(1)
	}
}
