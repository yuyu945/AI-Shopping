// Package config loads process configuration from the AI_SHOPPING_ environment namespace.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
)

const (
	mysqlDSNEnv                      = "AI_SHOPPING_MYSQL_DSN"
	redisAddrEnv                     = "AI_SHOPPING_REDIS_ADDR"
	kafkaBrokersEnv                  = "AI_SHOPPING_KAFKA_BROKERS"
	minIOEndpointEnv                 = "AI_SHOPPING_MINIO_ENDPOINT"
	minIOAccessKeyEnv                = "AI_SHOPPING_MINIO_ACCESS_KEY"
	minIOSecretKeyEnv                = "AI_SHOPPING_MINIO_SECRET_KEY"
	milvusAddressEnv                 = "AI_SHOPPING_MILVUS_ADDRESS"
	embeddingProviderEnv             = "AI_SHOPPING_EMBEDDING_PROVIDER"
	embeddingModelEnv                = "AI_SHOPPING_EMBEDDING_MODEL"
	embeddingDimensionEnv            = "AI_SHOPPING_EMBEDDING_DIMENSION"
	dashScopeAPIKeyEnv               = "AI_SHOPPING_DASHSCOPE_API_KEY"
	jwtSecretEnv                     = "AI_SHOPPING_JWT_SECRET"
	internalServiceTokenEnv          = "AI_SHOPPING_INTERNAL_SERVICE_TOKEN"
	minimumInternalServiceTokenBytes = 32
)

var errInvalidInternalServiceToken = errors.New("invalid internal service token")

var internalServiceTokenPlaceholders = map[string]struct{}{
	"REPLACE_WITH_SECRET_MANAGER_VALUE": {},
}

// Config contains the infrastructure endpoints required by the MVP services.
// Values are loaded only from AI_SHOPPING_ environment variables.
type Config struct {
	MySQLDSN             string
	RedisAddr            string
	KafkaBrokers         string
	MinIOEndpoint        string
	MinIOAccessKey       string
	MinIOSecretKey       string
	MilvusAddress        string
	EmbeddingProvider    string
	EmbeddingModel       string
	EmbeddingDimension   int
	DashScopeAPIKey      string
	JWTSecret            string
	InternalServiceToken string
}

// Load reads and validates the required AI_SHOPPING_ environment variables.
// Validation errors identify only the missing variable and never include values.
func Load() (Config, error) {
	values := make(map[string]string, 6)
	for _, name := range []string{
		mysqlDSNEnv,
		redisAddrEnv,
		kafkaBrokersEnv,
		minIOEndpointEnv,
		minIOAccessKeyEnv,
		minIOSecretKeyEnv,
		milvusAddressEnv,
		jwtSecretEnv,
	} {
		value := os.Getenv(name)
		if value == "" {
			return Config{}, fmt.Errorf("required environment variable %s is not set", name)
		}
		values[name] = value
	}

	embeddingDimension, err := optionalPositiveInt(embeddingDimensionEnv)
	if err != nil {
		return Config{}, err
	}

	return Config{
		MySQLDSN:             values[mysqlDSNEnv],
		RedisAddr:            values[redisAddrEnv],
		KafkaBrokers:         values[kafkaBrokersEnv],
		MinIOEndpoint:        values[minIOEndpointEnv],
		MinIOAccessKey:       values[minIOAccessKeyEnv],
		MinIOSecretKey:       values[minIOSecretKeyEnv],
		MilvusAddress:        values[milvusAddressEnv],
		EmbeddingProvider:    os.Getenv(embeddingProviderEnv),
		EmbeddingModel:       os.Getenv(embeddingModelEnv),
		EmbeddingDimension:   embeddingDimension,
		DashScopeAPIKey:      os.Getenv(dashScopeAPIKeyEnv),
		JWTSecret:            values[jwtSecretEnv],
		InternalServiceToken: os.Getenv(internalServiceTokenEnv),
	}, nil
}

func optionalPositiveInt(name string) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("environment variable %s must be a positive integer", name)
	}
	return parsed, nil
}

// ValidateInternalServiceToken verifies that an internal service token is a
// non-placeholder opaque ASCII secret suitable for service-to-service use.
// The caller must obtain the token from a secret manager or environment, not
// from a checked-in example file.
func ValidateInternalServiceToken(token string) error {
	if len(token) < minimumInternalServiceTokenBytes {
		return errInvalidInternalServiceToken
	}
	if _, isPlaceholder := internalServiceTokenPlaceholders[token]; isPlaceholder {
		return errInvalidInternalServiceToken
	}

	for i := 0; i < len(token); i++ {
		// Restrict the token to visible ASCII. This avoids whitespace and makes
		// metadata transport unambiguous while permitting random base64url values.
		if token[i] < '!' || token[i] > '~' {
			return errInvalidInternalServiceToken
		}
	}
	for i := 1; i < len(token); i++ {
		if token[i] != token[0] {
			return nil
		}
	}

	return errInvalidInternalServiceToken
}
