// Package logging builds the zap logger used by the server and worker.
package logging

import (
	"fmt"
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	samplingInitial    = 100
	samplingThereafter = 100
)

// New returns a production JSON logger that emits entries at or above level.
// level is parsed with zapcore.ParseLevel (for example "debug", "info", "warn"),
// and an unrecognised value returns an error that names it.
func New(level string) (*zap.Logger, error) {
	parsed, err := zapcore.ParseLevel(level)
	if err != nil {
		return nil, fmt.Errorf("invalid log level %q: %w", level, err)
	}

	core := zapcore.NewSamplerWithOptions(
		zapcore.NewCore(
			zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
			zapcore.Lock(os.Stderr),
			parsed,
		),
		time.Second, samplingInitial, samplingThereafter,
	)
	return zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel)), nil
}
