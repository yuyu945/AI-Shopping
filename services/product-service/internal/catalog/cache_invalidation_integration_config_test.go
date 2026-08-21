package catalog

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/google/uuid"
)

const cacheInvalidationIntegrationIsolationSentinel = "m12cacheverify"

type cacheInvalidationIntegrationSettings struct {
	mysqlDSN  string
	redisAddr string
	redisDB   int
	runID     string
}

func cacheInvalidationIntegrationConfig(getenv func(string) string) (cacheInvalidationIntegrationSettings, bool, error) {
	if getenv("AI_SHOPPING_INTEGRATION") != "1" {
		return cacheInvalidationIntegrationSettings{}, false, nil
	}
	if getenv("AI_SHOPPING_INTEGRATION_ISOLATED") != cacheInvalidationIntegrationIsolationSentinel {
		return cacheInvalidationIntegrationSettings{}, false, nil
	}

	config := cacheInvalidationIntegrationSettings{
		mysqlDSN:  getenv("AI_SHOPPING_MYSQL_DSN"),
		redisAddr: getenv("AI_SHOPPING_REDIS_ADDR"),
	}
	if config.mysqlDSN == "" || config.redisAddr == "" {
		return cacheInvalidationIntegrationSettings{}, false, fmt.Errorf("AI_SHOPPING_MYSQL_DSN and AI_SHOPPING_REDIS_ADDR are required")
	}

	redisDB, err := strconv.Atoi(getenv("AI_SHOPPING_REDIS_DB"))
	if err != nil || redisDB <= 0 {
		return cacheInvalidationIntegrationSettings{}, false, fmt.Errorf("AI_SHOPPING_REDIS_DB must be a nonzero Redis database index")
	}
	config.redisDB = redisDB

	runID, err := uuid.Parse(getenv("AI_SHOPPING_INTEGRATION_RUN_ID"))
	if err != nil {
		return cacheInvalidationIntegrationSettings{}, false, fmt.Errorf("AI_SHOPPING_INTEGRATION_RUN_ID must be a UUID")
	}
	config.runID = runID.String()
	return config, true, nil
}

func TestCacheInvalidationIntegrationConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		env       map[string]string
		wantRun   bool
		wantDB    int
		wantRunID string
		wantError bool
	}{
		{
			name:    "integration switch disabled skips",
			env:     map[string]string{},
			wantRun: false,
		},
		{
			name: "isolation sentinel mismatch skips",
			env: map[string]string{
				"AI_SHOPPING_INTEGRATION":          "1",
				"AI_SHOPPING_INTEGRATION_ISOLATED": "other",
			},
			wantRun: false,
		},
		{
			name: "missing run ID is rejected",
			env: map[string]string{
				"AI_SHOPPING_INTEGRATION":          "1",
				"AI_SHOPPING_INTEGRATION_ISOLATED": "m12cacheverify",
				"AI_SHOPPING_MYSQL_DSN":            "app:secret@tcp(127.0.0.1:3308)/catalog_db",
				"AI_SHOPPING_REDIS_ADDR":           "127.0.0.1:6381",
				"AI_SHOPPING_REDIS_DB":             "15",
			},
			wantError: true,
		},
		{
			name: "malformed run ID is rejected",
			env: map[string]string{
				"AI_SHOPPING_INTEGRATION":          "1",
				"AI_SHOPPING_INTEGRATION_ISOLATED": "m12cacheverify",
				"AI_SHOPPING_MYSQL_DSN":            "app:secret@tcp(127.0.0.1:3308)/catalog_db",
				"AI_SHOPPING_REDIS_ADDR":           "127.0.0.1:6381",
				"AI_SHOPPING_REDIS_DB":             "15",
				"AI_SHOPPING_INTEGRATION_RUN_ID":   "not-a-uuid",
			},
			wantError: true,
		},
		{
			name: "zero redis database is rejected",
			env: map[string]string{
				"AI_SHOPPING_INTEGRATION":          "1",
				"AI_SHOPPING_INTEGRATION_ISOLATED": "m12cacheverify",
				"AI_SHOPPING_MYSQL_DSN":            "app:secret@tcp(127.0.0.1:3308)/catalog_db",
				"AI_SHOPPING_REDIS_ADDR":           "127.0.0.1:6381",
				"AI_SHOPPING_REDIS_DB":             "0",
				"AI_SHOPPING_INTEGRATION_RUN_ID":   "79a5b82e-79bf-4f76-9b9d-3a5f478b5d29",
			},
			wantError: true,
		},
		{
			name: "invalid redis database is rejected",
			env: map[string]string{
				"AI_SHOPPING_INTEGRATION":          "1",
				"AI_SHOPPING_INTEGRATION_ISOLATED": "m12cacheverify",
				"AI_SHOPPING_MYSQL_DSN":            "app:secret@tcp(127.0.0.1:3308)/catalog_db",
				"AI_SHOPPING_REDIS_ADDR":           "127.0.0.1:6381",
				"AI_SHOPPING_REDIS_DB":             "fifteen",
				"AI_SHOPPING_INTEGRATION_RUN_ID":   "79a5b82e-79bf-4f76-9b9d-3a5f478b5d29",
			},
			wantError: true,
		},
		{
			name: "isolated configuration is accepted",
			env: map[string]string{
				"AI_SHOPPING_INTEGRATION":          "1",
				"AI_SHOPPING_INTEGRATION_ISOLATED": "m12cacheverify",
				"AI_SHOPPING_MYSQL_DSN":            "app:secret@tcp(127.0.0.1:3308)/catalog_db",
				"AI_SHOPPING_REDIS_ADDR":           "127.0.0.1:6381",
				"AI_SHOPPING_REDIS_DB":             "15",
				"AI_SHOPPING_INTEGRATION_RUN_ID":   "79a5b82e-79bf-4f76-9b9d-3a5f478b5d29",
			},
			wantRun:   true,
			wantDB:    15,
			wantRunID: "79a5b82e-79bf-4f76-9b9d-3a5f478b5d29",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, run, err := cacheInvalidationIntegrationConfig(func(key string) string {
				return tt.env[key]
			})
			if (err != nil) != tt.wantError {
				t.Fatalf("cacheInvalidationIntegrationConfig() error = %v, wantError %v", err, tt.wantError)
			}
			if run != tt.wantRun {
				t.Fatalf("cacheInvalidationIntegrationConfig() run = %v, want %v", run, tt.wantRun)
			}
			if config.redisDB != tt.wantDB {
				t.Fatalf("cacheInvalidationIntegrationConfig() redis DB = %d, want %d", config.redisDB, tt.wantDB)
			}
			if config.runID != tt.wantRunID {
				t.Fatalf("cacheInvalidationIntegrationConfig() run ID = %q, want %q", config.runID, tt.wantRunID)
			}
		})
	}
}
