package knowledge

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

const knowledgeIntegrationIsolationSentinel = "m32knowledge"

type knowledgeIntegrationSettings struct {
	mysqlDSN        string
	runID           string
	minIOEndpoint   string
	minIOAccessKey  string
	minIOSecretKey  string
	minIOBucket     string
	kafkaBrokers    []string
	milvusAddress   string
	dashScopeAPIKey string
}

func knowledgeIntegrationConfig(getenv func(string) string) (knowledgeIntegrationSettings, bool, error) {
	if getenv("AI_SHOPPING_INTEGRATION") != "1" || getenv("AI_SHOPPING_INTEGRATION_ISOLATED") != knowledgeIntegrationIsolationSentinel {
		return knowledgeIntegrationSettings{}, false, nil
	}

	config := knowledgeIntegrationSettings{
		mysqlDSN:        getenv("AI_SHOPPING_MYSQL_DSN"),
		minIOEndpoint:   getenv("AI_SHOPPING_MINIO_ENDPOINT"),
		minIOAccessKey:  getenv("AI_SHOPPING_MINIO_ACCESS_KEY"),
		minIOSecretKey:  getenv("AI_SHOPPING_MINIO_SECRET_KEY"),
		minIOBucket:     getenv("AI_SHOPPING_MINIO_BUCKET"),
		milvusAddress:   getenv("AI_SHOPPING_MILVUS_ADDRESS"),
		dashScopeAPIKey: getenv("AI_SHOPPING_DASHSCOPE_API_KEY"),
	}
	if config.minIOBucket == "" {
		config.minIOBucket = "knowledge"
	}
	kafkaBrokers := strings.Split(getenv("AI_SHOPPING_KAFKA_BROKERS"), ",")
	for _, broker := range kafkaBrokers {
		if broker = strings.TrimSpace(broker); broker != "" {
			config.kafkaBrokers = append(config.kafkaBrokers, broker)
		}
	}
	if config.mysqlDSN == "" || config.minIOEndpoint == "" || config.minIOAccessKey == "" || config.minIOSecretKey == "" || len(config.kafkaBrokers) == 0 || config.milvusAddress == "" || config.dashScopeAPIKey == "" {
		return knowledgeIntegrationSettings{}, false, fmt.Errorf("knowledge integration dependency environment is incomplete")
	}

	mysqlConfig, err := mysql.ParseDSN(config.mysqlDSN)
	if err != nil || mysqlConfig.DBName != "knowledge_db" {
		return knowledgeIntegrationSettings{}, false, fmt.Errorf("AI_SHOPPING_MYSQL_DSN must target knowledge_db")
	}
	if err := validateKnowledgeIntegrationMySQLAddress(mysqlConfig); err != nil {
		return knowledgeIntegrationSettings{}, false, err
	}

	runID, err := uuid.Parse(getenv("AI_SHOPPING_INTEGRATION_RUN_ID"))
	if err != nil {
		return knowledgeIntegrationSettings{}, false, fmt.Errorf("AI_SHOPPING_INTEGRATION_RUN_ID must be a UUID")
	}
	config.runID = runID.String()
	return config, true, nil
}

func validateKnowledgeIntegrationMySQLAddress(config *mysql.Config) error {
	if config.Net != "tcp" {
		return fmt.Errorf("AI_SHOPPING_MYSQL_DSN must use TCP")
	}
	host, port, err := net.SplitHostPort(config.Addr)
	if err != nil {
		return fmt.Errorf("AI_SHOPPING_MYSQL_DSN must include a host and port: %w", err)
	}
	portNumber, err := strconv.ParseUint(port, 10, 16)
	if err != nil || portNumber == 0 || portNumber == 3306 {
		return fmt.Errorf("AI_SHOPPING_MYSQL_DSN must use a non-default MySQL port")
	}
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return fmt.Errorf("AI_SHOPPING_MYSQL_DSN host must be localhost, 127.0.0.1, or [::1]")
	}
	return nil
}
