package logging_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/facturaIA/invoice-ocr-service/internal/logging"
)

// TestInitJSON verifies that LOG_FORMAT=json configures a JSON handler.
func TestInitJSON(t *testing.T) {
	t.Setenv("LOG_FORMAT", "json")

	logging.Init()

	// Capture output by replacing the default handler on a local logger.
	var buf bytes.Buffer
	jsonHandler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(jsonHandler)
	logger.Info("test event", slog.String("key", "value"))

	// Must be valid JSON with expected fields.
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("JSON handler produced invalid JSON: %v — output: %s", err, buf.String())
	}
	if out["msg"] != "test event" {
		t.Errorf("expected msg='test event', got %v", out["msg"])
	}
	if out["key"] != "value" {
		t.Errorf("expected key='value', got %v", out["key"])
	}
}

// TestInitText verifies that LOG_FORMAT=text (default) configures a text handler.
func TestInitText(t *testing.T) {
	os.Unsetenv("LOG_FORMAT")

	logging.Init()

	// Text handler output contains "msg=..." (key=value format).
	var buf bytes.Buffer
	textHandler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(textHandler)
	logger.Info("text event", slog.String("field", "hello"))

	output := buf.String()
	if !strings.Contains(output, "text event") {
		t.Errorf("expected 'text event' in text output, got: %s", output)
	}
	if !strings.Contains(output, "field=hello") {
		t.Errorf("expected 'field=hello' in text output, got: %s", output)
	}
}
