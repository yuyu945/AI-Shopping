package order

import (
	"fmt"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

const orderSnapshotIntegrationIsolationSentinel = "m21orderverify"

type orderSnapshotIntegrationSettings struct {
	mysqlDSN string
	runID    string
}

func orderSnapshotIntegrationConfig(getenv func(string) string) (orderSnapshotIntegrationSettings, bool, error) {
	if getenv("AI_SHOPPING_INTEGRATION") != "1" || getenv("AI_SHOPPING_INTEGRATION_ISOLATED") != orderSnapshotIntegrationIsolationSentinel {
		return orderSnapshotIntegrationSettings{}, false, nil
	}

	dsn := getenv("AI_SHOPPING_MYSQL_DSN")
	config, err := mysql.ParseDSN(dsn)
	if err != nil || config.DBName != "trade_db" {
		return orderSnapshotIntegrationSettings{}, false, fmt.Errorf("AI_SHOPPING_MYSQL_DSN must target trade_db")
	}
	runID, err := uuid.Parse(getenv("AI_SHOPPING_INTEGRATION_RUN_ID"))
	if err != nil {
		return orderSnapshotIntegrationSettings{}, false, fmt.Errorf("AI_SHOPPING_INTEGRATION_RUN_ID must be a UUID")
	}
	return orderSnapshotIntegrationSettings{mysqlDSN: dsn, runID: runID.String()}, true, nil
}

func integrationDSNForDatabase(dsn, database string) (string, error) {
	config, err := mysql.ParseDSN(dsn)
	if err != nil {
		return "", fmt.Errorf("parse integration DSN: %w", err)
	}
	config.DBName = database
	return config.FormatDSN(), nil
}
