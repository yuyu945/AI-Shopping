// Package serviceconfig loads the minimal configuration shared by executable service skeletons.
package serviceconfig

import (
	"fmt"

	"github.com/zeromicro/go-zero/core/conf"
)

// Config is the minimum process configuration for a service executable.
type Config struct {
	Name     string
	ListenOn string
}

// Load reads and validates the configuration file for expectedServiceName.
func Load(path, expectedServiceName string) (Config, error) {
	var config Config
	if err := conf.Load(path, &config); err != nil {
		return Config{}, fmt.Errorf("%s load configuration: %w", expectedServiceName, err)
	}
	if config.Name != expectedServiceName {
		return Config{}, fmt.Errorf("%s validate configuration: Name must be %q", expectedServiceName, expectedServiceName)
	}
	if config.ListenOn == "" {
		return Config{}, fmt.Errorf("%s validate configuration: ListenOn is required", expectedServiceName)
	}
	return config, nil
}
