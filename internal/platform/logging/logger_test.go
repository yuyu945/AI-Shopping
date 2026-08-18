package logging_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/yuyu945/AI-Shopping/internal/platform/logging"
)

func TestNewWritesJSONLogRecords(t *testing.T) {
	var output bytes.Buffer
	logger := logging.New(&output, slog.LevelInfo)
	logger.Info("service started", "service", "catalog")

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("log output is not JSON: %v", err)
	}
	if got := record["msg"]; got != "service started" {
		t.Errorf("msg = %v, want service started", got)
	}
	if got := record["service"]; got != "catalog" {
		t.Errorf("service = %v, want catalog", got)
	}
}
