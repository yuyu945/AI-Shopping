package serviceconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadReadsServiceConfiguration(t *testing.T) {
	path := writeConfig(t, "Name: user-service\nListenOn: 127.0.0.1:10001\n")

	got, err := Load(path, "user-service")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Name != "user-service" {
		t.Errorf("Name = %q, want user-service", got.Name)
	}
	if got.ListenOn != "127.0.0.1:10001" {
		t.Errorf("ListenOn = %q, want 127.0.0.1:10001", got.ListenOn)
	}
}

func TestLoadRejectsUnexpectedServiceName(t *testing.T) {
	path := writeConfig(t, "Name: product-service\nListenOn: 127.0.0.1:10002\n")

	_, err := Load(path, "user-service")
	if err == nil {
		t.Fatal("Load() error = nil, want service name validation error")
	}
	if !strings.Contains(err.Error(), "user-service") {
		t.Errorf("Load() error = %q, want expected service name", err)
	}
}

func TestLoadRejectsMissingListenOn(t *testing.T) {
	path := writeConfig(t, "Name: user-service\n")

	_, err := Load(path, "user-service")
	if err == nil {
		t.Fatal("Load() error = nil, want ListenOn validation error")
	}
	if !strings.Contains(err.Error(), "ListenOn") {
		t.Errorf("Load() error = %q, want ListenOn", err)
	}
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "service.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write test config: %v", err)
	}
	return path
}
