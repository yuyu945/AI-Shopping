package main

import (
	"flag"
	"log"

	platformconfig "github.com/yuyu945/AI-Shopping/internal/platform/config"
	"github.com/yuyu945/AI-Shopping/internal/platform/serviceconfig"
)

const SERVICE_NAME = "agent-service"

func main() {
	var configFile string
	flag.StringVar(&configFile, "f", "services/agent-service/etc/agent-service.yaml", "Service configuration file")
	flag.Parse()

	config, err := serviceconfig.Load(configFile, SERVICE_NAME)
	if err != nil {
		log.Fatalf("%s startup: %v", SERVICE_NAME, err)
	}
	if _, err := platformconfig.Load(); err != nil {
		log.Fatalf("%s load runtime configuration: %v", SERVICE_NAME, err)
	}

	// This executable validates startup configuration only; M1.2 adds Go-zero RPC after proto definitions exist.
	log.Printf("%s executable skeleton configured for %s", SERVICE_NAME, config.ListenOn)
}
