package database

import (
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/lib/pq"
)

// InitDatabase checks if clustereye database exists and creates it if not
func InitDatabase(cfg Config) (*sql.DB, error) {
	// First connect to postgres database to check/create clustereye database
	tempCfg := cfg
	tempCfg.DBName = "postgres" // Connect to default postgres database

	connStr := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		tempCfg.Host, tempCfg.Port, tempCfg.User, tempCfg.Password, tempCfg.DBName, tempCfg.SSLMode,
	)

	tempDB, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("temporary database connection failed: %w", err)
	}
	defer tempDB.Close()

	// Check if clustereye database exists
	var exists bool
	query := "SELECT EXISTS(SELECT datname FROM pg_catalog.pg_database WHERE datname = $1)"
	err = tempDB.QueryRow(query, "clustereye").Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("failed to check database existence: %w", err)
	}

	// Create clustereye database if it doesn't exist
	if !exists {
		fmt.Println("clustereye database not found, creating...")
		_, err = tempDB.Exec("CREATE DATABASE clustereye")
		if err != nil {
			return nil, fmt.Errorf("failed to create clustereye database: %w", err)
		}
		fmt.Println("clustereye database created successfully")
	} else {
		fmt.Println("clustereye database already exists")
	}

	// Now connect to clustereye database
	cfg.DBName = "clustereye"
	db, err := ConnectDatabase(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to clustereye database: %w", err)
	}

	return db, nil
}

// InitAllTables creates all required tables for ClusterEye
func InitAllTables(db *sql.DB) error {
	fmt.Println("Initializing database tables...")

	// Create helper function for updated_at triggers
	if err := createUpdateTriggerFunction(db); err != nil {
		return fmt.Errorf("failed to create update trigger function: %w", err)
	}

	// List of tables to create in order (respecting foreign key dependencies)
	tables := []struct {
		name string
		sql  string
	}{
		{"companies", createCompaniesTable()},
		{"agents", createAgentsTable()},
		{"agent_tokens", createAgentTokensTable()},
		{"jobs", createJobsTable()},
		{"mssql_health_reports", createMssqlHealthReportsTable()},
		{"agent_versions", createAgentVersionsTable()},
		{"agentstatus", createAgentStatusTable()},
		{"alarms", createAlarmsTable()},
		{"api_versions", createApiVersionsTable()},
		{"mongo_data", createMongoDataTable()},
		{"mongodb_logs", createMongodbLogsTable()},
		{"mongojsondata", createMongoJsonDataTable()},
		{"mssql_data", createMssqlDataTable()},
		{"node_status", createNodeStatusTable()},
		{"notification_settings", createNotificationSettingsTable()},
		{"operation_logs", createOperationLogsTable()},
		{"postgres_data", createPostgresDataTable()},
		{"process_logs", createProcessLogsTable()},
		{"rds_mssql_data", createRdsMssqlDataTable()},
		{"replsetinfo", createReplSetInfoTable()},
		{"settings", createSettingsTable()},
		{"threshold_settings", createThresholdSettingsTable()},
		{"users", createUsersTable()},
	}

	// Create tables
	for _, table := range tables {
		fmt.Printf("Creating table: %s...\n", table.name)
		if err := executeSQL(db, table.sql); err != nil {
			return fmt.Errorf("failed to create table %s: %w", table.name, err)
		}
		fmt.Printf("Table created successfully: %s\n", table.name)
	}

	fmt.Println("All tables initialized successfully")
	return nil
}

// executeSQL executes a SQL statement and handles errors
func executeSQL(db *sql.DB, sqlStatement string) error {
	// Split multiple statements if they exist
	statements := strings.Split(sqlStatement, ";\n")

	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}

		_, err := db.Exec(stmt)
		if err != nil {
			return fmt.Errorf("SQL execution failed for statement: %s\nError: %w", stmt, err)
		}
	}

	return nil
}

// createUpdateTriggerFunction creates the trigger function for updated_at columns
func createUpdateTriggerFunction(db *sql.DB) error {
	sql := `CREATE OR REPLACE FUNCTION update_updated_at_column()
	RETURNS TRIGGER AS $$
	BEGIN
		NEW.updated_at = CURRENT_TIMESTAMP;
		RETURN NEW;
	END;
	$$ language 'plpgsql';`

	return executeSQL(db, sql)
}