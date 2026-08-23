package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAgentServiceConfigLoadsRuntimeSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-service.yaml")
	contents := []byte(`
Name: agent-service
ListenOn: 127.0.0.1:19005
Timeout: 2000
Runtime:
  MaxSteps: 8
  RunTimeout: 30s
  ToolTimeout: 2s
ProductRPC:
  Endpoints:
    - 127.0.0.1:19002
UserRPC:
  Endpoints:
    - 127.0.0.1:19001
KnowledgeRPC:
  Endpoints:
    - 127.0.0.1:19004
`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := loadAgentServiceConfig(path)
	if err != nil {
		t.Fatalf("loadAgentServiceConfig() error = %v", err)
	}
	if config.Name != SERVICE_NAME || config.ListenOn != "127.0.0.1:19005" {
		t.Fatalf("config identity = %#v", config.RpcServerConf)
	}
	if config.Runtime.MaxSteps != 8 || config.Runtime.RunTimeout != 30*time.Second || config.Runtime.ToolTimeout != 2*time.Second {
		t.Fatalf("runtime config = %#v", config.Runtime)
	}
	if len(config.ProductRPC.Endpoints) != 1 || len(config.UserRPC.Endpoints) != 1 || len(config.KnowledgeRPC.Endpoints) != 1 {
		t.Fatalf("rpc config = product:%#v user:%#v knowledge:%#v", config.ProductRPC, config.UserRPC, config.KnowledgeRPC)
	}
}
