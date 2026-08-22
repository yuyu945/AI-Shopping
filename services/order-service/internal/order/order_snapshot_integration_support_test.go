package order

import (
	"fmt"
	"net"
	"strconv"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

const orderSnapshotIntegrationIsolationSentinel = "m21ordersnapshot"

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
	if err := validateOrderSnapshotIntegrationAddress(config); err != nil {
		return orderSnapshotIntegrationSettings{}, false, err
	}
	runID, err := uuid.Parse(getenv("AI_SHOPPING_INTEGRATION_RUN_ID"))
	if err != nil {
		return orderSnapshotIntegrationSettings{}, false, fmt.Errorf("AI_SHOPPING_INTEGRATION_RUN_ID must be a UUID")
	}
	return orderSnapshotIntegrationSettings{mysqlDSN: dsn, runID: runID.String()}, true, nil
}

func validateOrderSnapshotIntegrationAddress(config *mysql.Config) error {
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

func integrationDSNForDatabase(dsn, database string) (string, error) {
	config, err := mysql.ParseDSN(dsn)
	if err != nil {
		return "", fmt.Errorf("parse integration DSN: %w", err)
	}
	config.DBName = database
	return config.FormatDSN(), nil
}
