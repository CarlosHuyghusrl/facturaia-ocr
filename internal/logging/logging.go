// Package logging initialises the global slog logger.
//
// LOG_FORMAT env var:
//
//	"json" → structured JSON output (production / observability tools)
//	"text" → human-readable text output (default / development)
//
// Call logging.Init() once from main() before any other code runs.
// All code can then use slog.Info/Warn/Error directly — they pick up the
// default handler set here.
package logging

import (
	"log/slog"
	"os"
)

// Init configures the global slog default based on LOG_FORMAT env var.
func Init() {
	var handler slog.Handler
	if os.Getenv("LOG_FORMAT") == "json" {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})
	} else {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})
	}
	slog.SetDefault(slog.New(handler))
}
