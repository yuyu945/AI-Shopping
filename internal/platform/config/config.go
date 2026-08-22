// Package config loads process configuration from the AI_SHOPPING_ environment namespace.
package config

import (
	"fmt"
	"os"
)

const (
	mysqlDSNEnv             = "AI_SHOPPING_MYSQL_DSN"
	redisAddrEnv            = "AI_SHOPPING_REDIS_ADDR"
	kafkaBrokersEnv         = "AI_SHOPPING_KAFKA_BROKERS"
	minIOEndpointEnv        = "AI_SHOPPING_MINIO_ENDPOINT"
	milvusAddressEnv        = "AI_SHOPPING_MILVUS_ADDRESS"
	jwtSecretEnv            = "AI_SHOPPING_JWT_SECRET"
	internalServiceTokenEnv = "AI_SHOPPING_INTERNAL_SERVICE_TOKEN"
)

// Config contains the infrastructure endpoints required by the MVP services.
// Values are loaded only from AI_SHOPPING_ environment variables.
type Config struct {
	MySQLDSN             string
	RedisAddr            string
	KafkaBrokers         string
	MinIOEndpoint        string
	MilvusAddress        string
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
		milvusAddressEnv,
		jwtSecretEnv,
	} {
		value := os.Getenv(name)
		if value == "" {
			return Config{}, fmt.Errorf("required environment variable %s is not set", name)
		}
		values[name] = value
	}

	return Config{
		MySQLDSN:             values[mysqlDSNEnv],
		RedisAddr:            values[redisAddrEnv],
		KafkaBrokers:         values[kafkaBrokersEnv],
		MinIOEndpoint:        values[minIOEndpointEnv],
		MilvusAddress:        values[milvusAddressEnv],
		JWTSecret:            values[jwtSecretEnv],
		InternalServiceToken: os.Getenv(internalServiceTokenEnv),
	}, nil
}
