package config_test

import (
	"os"
	"strings"
	"testing"

	"github.com/yuyu945/AI-Shopping/internal/platform/config"
)

var requiredEnvironment = map[string]string{
	"AI_SHOPPING_MYSQL_DSN":      "user:password@tcp(mysql:3306)/app",
	"AI_SHOPPING_REDIS_ADDR":     "redis:6379",
	"AI_SHOPPING_KAFKA_BROKERS":  "kafka-1:9092,kafka-2:9092",
	"AI_SHOPPING_MINIO_ENDPOINT": "minio:9000",
	"AI_SHOPPING_MILVUS_ADDRESS": "milvus:19530",
	"AI_SHOPPING_JWT_SECRET":     "01234567890123456789012345678901",
}

func setRequiredEnvironment(t *testing.T) {
	t.Helper()
	for name, value := range requiredEnvironment {
		t.Setenv(name, value)
	}
}

func TestLoadFailsForEachMissingRequiredEnvironment(t *testing.T) {
	for missing := range requiredEnvironment {
		t.Run(missing, func(t *testing.T) {
			setRequiredEnvironment(t)
			t.Setenv(missing, "")

			_, err := config.Load()
			if err == nil {
				t.Fatal("Load() error = nil, want missing required environment error")
			}
			if !strings.Contains(err.Error(), missing) {
				t.Fatalf("Load() error = %q, want variable name %q", err, missing)
			}
			for _, configuredValue := range requiredEnvironment {
				if strings.Contains(err.Error(), configuredValue) {
					t.Fatalf("Load() error = %q, must not contain configuration values", err)
				}
			}
		})
	}
}

func TestLoadReadsCompleteRequiredEnvironment(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("MYSQL_DSN", "must-not-be-read")

	got, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got.MySQLDSN != requiredEnvironment["AI_SHOPPING_MYSQL_DSN"] {
		t.Errorf("MySQLDSN = %q, want configured value", got.MySQLDSN)
	}
	if got.RedisAddr != requiredEnvironment["AI_SHOPPING_REDIS_ADDR"] {
		t.Errorf("RedisAddr = %q, want configured value", got.RedisAddr)
	}
	if got.KafkaBrokers != requiredEnvironment["AI_SHOPPING_KAFKA_BROKERS"] {
		t.Errorf("KafkaBrokers = %q, want configured value", got.KafkaBrokers)
	}
	if got.MinIOEndpoint != requiredEnvironment["AI_SHOPPING_MINIO_ENDPOINT"] {
		t.Errorf("MinIOEndpoint = %q, want configured value", got.MinIOEndpoint)
	}
	if got.MilvusAddress != requiredEnvironment["AI_SHOPPING_MILVUS_ADDRESS"] {
		t.Errorf("MilvusAddress = %q, want configured value", got.MilvusAddress)
	}
	if got.JWTSecret != requiredEnvironment["AI_SHOPPING_JWT_SECRET"] {
		t.Errorf("JWTSecret = %q, want configured value", got.JWTSecret)
	}
}

func TestLoadReadsInternalServiceTokenWithoutMakingItGlobalRequirement(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("AI_SHOPPING_INTERNAL_SERVICE_TOKEN", "szF1wQ8oXn7uK4mV2rC9yT5pL6dE3aB0")

	got, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.InternalServiceToken != "szF1wQ8oXn7uK4mV2rC9yT5pL6dE3aB0" {
		t.Fatalf("InternalServiceToken = %q, want configured value", got.InternalServiceToken)
	}
}

func TestValidateInternalServiceToken(t *testing.T) {
	validToken := "szF1wQ8oXn7uK4mV2rC9yT5pL6dE3aB0"
	tests := []struct {
		name  string
		token string
		valid bool
	}{
		{name: "empty", token: ""},
		{name: "example placeholder", token: "REPLACE_WITH_SECRET_MANAGER_VALUE"},
		{name: "too short", token: "szF1wQ8oXn7uK4mV2rC9yT5pL6dE3"},
		{name: "contains whitespace", token: "szF1wQ8oXn7uK4mV2rC9yT5pL6dE3a B"},
		{name: "insufficient entropy signal", token: strings.Repeat("a", 32)},
		{name: "valid random opaque token", token: validToken, valid: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := config.ValidateInternalServiceToken(tt.token)
			if (err == nil) != tt.valid {
				t.Fatalf("ValidateInternalServiceToken() error = %v, valid = %v", err, tt.valid)
			}
			if err != nil && tt.token != "" && strings.Contains(err.Error(), tt.token) {
				t.Fatalf("ValidateInternalServiceToken() error = %q, leaked token", err)
			}
		})
	}
}

func TestExampleEnvironmentSatisfiesLoadRequirements(t *testing.T) {
	contents, err := os.ReadFile("../../../.env.example")
	if err != nil {
		t.Fatalf("read .env.example: %v", err)
	}

	example := parseEnvironmentFile(t, string(contents))
	for name := range requiredEnvironment {
		t.Setenv(name, "")
		value, ok := example[name]
		if !ok || value == "" {
			t.Fatalf(".env.example missing non-empty %s", name)
		}
		t.Setenv(name, value)
	}
	for name := range example {
		if strings.HasPrefix(name, "AI_SHOPPING_") {
			if _, ok := requiredEnvironment[name]; !ok && name != "AI_SHOPPING_INTERNAL_SERVICE_TOKEN" {
				t.Fatalf(".env.example contains unsupported runtime variable %s", name)
			}
		}
	}

	if _, err := config.Load(); err != nil {
		t.Fatalf("Load() with .env.example runtime variables: %v", err)
	}
	if err := config.ValidateInternalServiceToken(example["AI_SHOPPING_INTERNAL_SERVICE_TOKEN"]); err == nil {
		t.Fatal(".env.example internal service token placeholder must be rejected for product-service startup")
	}
}

func parseEnvironmentFile(t *testing.T, contents string) map[string]string {
	t.Helper()
	values := make(map[string]string)
	for _, line := range strings.Split(contents, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("invalid .env.example line %q", line)
		}
		values[name] = value
	}
	return values
}
