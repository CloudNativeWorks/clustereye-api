package database

// createCompaniesTable returns SQL for companies table
func createCompaniesTable() string {
	return `
CREATE TABLE IF NOT EXISTS companies (
	id serial4 NOT NULL,
	company_name varchar(255) NOT NULL,
	agent_key varchar(64) NOT NULL,
	expiration_date timestamp NOT NULL,
	created_at timestamp DEFAULT CURRENT_TIMESTAMP NULL,
	updated_at timestamp DEFAULT CURRENT_TIMESTAMP NULL,
	CONSTRAINT companies_agent_key_key UNIQUE (agent_key),
	CONSTRAINT companies_pkey PRIMARY KEY (id)
);`
}

// createAgentsTable returns SQL for agents table
func createAgentsTable() string {
	return `
CREATE TABLE IF NOT EXISTS agents (
	id serial4 NOT NULL,
	company_id int4 NULL,
	agent_id varchar(64) NOT NULL,
	hostname varchar(255) NOT NULL,
	ip varchar(45) NOT NULL,
	last_seen timestamp DEFAULT CURRENT_TIMESTAMP NULL,
	status varchar(20) DEFAULT 'active'::character varying NULL,
	created_at timestamp DEFAULT CURRENT_TIMESTAMP NULL,
	updated_at timestamp DEFAULT CURRENT_TIMESTAMP NULL,
	CONSTRAINT agents_agent_id_key UNIQUE (agent_id),
	CONSTRAINT agents_pkey PRIMARY KEY (id),
	CONSTRAINT agents_company_id_fkey FOREIGN KEY (company_id) REFERENCES companies(id)
);`
}

// createAgentTokensTable returns SQL for agent_tokens table
func createAgentTokensTable() string {
	return `
CREATE TABLE IF NOT EXISTS agent_tokens (
	id serial4 NOT NULL,
	agent_id varchar(255) NOT NULL,
	company_id int4 NOT NULL,
	"token" text NOT NULL,
	expires_at timestamptz NOT NULL,
	created_at timestamptz NOT NULL,
	updated_at timestamptz NOT NULL,
	is_revoked bool DEFAULT false NOT NULL,
	CONSTRAINT agent_tokens_pkey PRIMARY KEY (id),
	CONSTRAINT agent_tokens_company_id_fkey FOREIGN KEY (company_id) REFERENCES companies(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_agent_tokens_agent_id ON agent_tokens USING btree (agent_id);
CREATE INDEX IF NOT EXISTS idx_agent_tokens_expires_at ON agent_tokens USING btree (expires_at);
CREATE INDEX IF NOT EXISTS idx_agent_tokens_token ON agent_tokens USING btree (token);`
}

// createJobsTable returns SQL for jobs table
func createJobsTable() string {
	return `
CREATE TABLE IF NOT EXISTS jobs (
	job_id varchar(36) NOT NULL,
	"type" varchar(50) NOT NULL,
	status varchar(20) NOT NULL,
	agent_id varchar(64) NOT NULL,
	created_at timestamptz NOT NULL,
	updated_at timestamptz NOT NULL,
	error_message text NULL,
	parameters jsonb NULL,
	"result" text NULL,
	can_rollback bool DEFAULT false NULL,
	rollback_state jsonb NULL,
	CONSTRAINT jobs_pkey PRIMARY KEY (job_id),
	CONSTRAINT fk_agent FOREIGN KEY (agent_id) REFERENCES agents(agent_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_jobs_agent_id ON jobs USING btree (agent_id);
CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs USING btree (created_at);
CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs USING btree (status);
CREATE INDEX IF NOT EXISTS idx_jobs_type ON jobs USING btree (type);`
}

// createMssqlHealthReportsTable returns SQL for mssql_health_reports table
func createMssqlHealthReportsTable() string {
	return `
CREATE TABLE IF NOT EXISTS mssql_health_reports (
	id serial4 NOT NULL,
	job_id varchar(255) NOT NULL,
	agent_id varchar(255) NOT NULL,
	report_data jsonb NOT NULL,
	created_at timestamptz DEFAULT now() NOT NULL,
	CONSTRAINT mssql_health_reports_pkey PRIMARY KEY (id),
	CONSTRAINT fk_job_id FOREIGN KEY (job_id) REFERENCES jobs(job_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_mssql_health_reports_agent_id ON mssql_health_reports USING btree (agent_id);
CREATE INDEX IF NOT EXISTS idx_mssql_health_reports_job_id ON mssql_health_reports USING btree (job_id);`
}

// createAgentVersionsTable returns SQL for agent_versions table
func createAgentVersionsTable() string {
	return `
CREATE TABLE IF NOT EXISTS agent_versions (
	id serial4 NOT NULL,
	agent_id varchar(255) NOT NULL,
	"version" varchar(50) NOT NULL,
	platform varchar(50) NOT NULL,
	architecture varchar(50) NOT NULL,
	hostname varchar(255) NOT NULL,
	os_version varchar(255) NOT NULL,
	go_version varchar(50) NOT NULL,
	reported_at timestamptz NOT NULL,
	created_at timestamptz DEFAULT CURRENT_TIMESTAMP NULL,
	updated_at timestamptz DEFAULT CURRENT_TIMESTAMP NULL,
	CONSTRAINT agent_versions_agent_id_key UNIQUE (agent_id),
	CONSTRAINT agent_versions_pkey PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS idx_agent_versions_agent_id ON agent_versions USING btree (agent_id);
CREATE INDEX IF NOT EXISTS idx_agent_versions_reported_at ON agent_versions USING btree (reported_at);
DROP TRIGGER IF EXISTS update_agent_versions_updated_at ON agent_versions;
CREATE TRIGGER update_agent_versions_updated_at BEFORE UPDATE ON agent_versions FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();`
}

// createAgentStatusTable returns SQL for agentstatus table
func createAgentStatusTable() string {
	return `
CREATE TABLE IF NOT EXISTS agentstatus (
	id bigserial NOT NULL,
	nodename varchar NULL,
	datetimes timestamp NULL,
	CONSTRAINT agentstatus_un UNIQUE (nodename),
	CONSTRAINT mongojsondata_pkey_1_1 PRIMARY KEY (id)
);
CREATE UNIQUE INDEX IF NOT EXISTS replsetinfo_nodename_idx_1 ON agentstatus USING btree (nodename);`
}

// createAlarmsTable returns SQL for alarms table
func createAlarmsTable() string {
	return `
CREATE TABLE IF NOT EXISTS alarms (
	id serial4 NOT NULL,
	alarm_id varchar(255) NOT NULL,
	event_id varchar(255) NOT NULL,
	agent_id varchar(255) NOT NULL,
	status varchar(50) NOT NULL,
	metric_name varchar(255) NOT NULL,
	metric_value varchar(255) NOT NULL,
	message text NOT NULL,
	severity varchar(50) NOT NULL,
	created_at timestamp DEFAULT CURRENT_TIMESTAMP NOT NULL,
	updated_at timestamp DEFAULT CURRENT_TIMESTAMP NOT NULL,
	acknowledged bool DEFAULT false NULL,
	"database" varchar(255) NULL,
	CONSTRAINT alarms_pkey PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS idx_alarms_acknowledged ON alarms USING btree (acknowledged);
CREATE INDEX IF NOT EXISTS idx_alarms_acknowledged_created_at ON alarms USING btree (acknowledged, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_alarms_agent_id ON alarms USING btree (agent_id);
CREATE INDEX IF NOT EXISTS idx_alarms_agent_id_created_at ON alarms USING btree (agent_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_alarms_created_at ON alarms USING btree (created_at);
CREATE INDEX IF NOT EXISTS idx_alarms_metric_name ON alarms USING btree (metric_name);
CREATE INDEX IF NOT EXISTS idx_alarms_severity ON alarms USING btree (severity);
CREATE INDEX IF NOT EXISTS idx_alarms_severity_created_at ON alarms USING btree (severity, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_alarms_status ON alarms USING btree (status);
CREATE INDEX IF NOT EXISTS idx_alarms_unacknowledged_recent ON alarms USING btree (created_at DESC) WHERE (acknowledged = false);`
}

// createApiVersionsTable returns SQL for api_versions table
func createApiVersionsTable() string {
	return `
CREATE TABLE IF NOT EXISTS api_versions (
	id serial4 NOT NULL,
	"version" varchar NULL,
	url varchar NULL,
	CONSTRAINT errorlog_pkey_1_1 PRIMARY KEY (id)
);`
}

// createMongoDataTable returns SQL for mongo_data table
func createMongoDataTable() string {
	return `
CREATE TABLE IF NOT EXISTS mongo_data (
	id serial4 NOT NULL,
	jsondata jsonb NOT NULL,
	clustername varchar(255) NOT NULL,
	created_at timestamp DEFAULT CURRENT_TIMESTAMP NOT NULL,
	updated_at timestamp DEFAULT CURRENT_TIMESTAMP NOT NULL,
	CONSTRAINT mongo_data_pkey PRIMARY KEY (id),
	CONSTRAINT unique_mongo_clustername UNIQUE (clustername)
);
CREATE INDEX IF NOT EXISTS idx_mongo_data_created_at ON mongo_data USING btree (created_at);`
}

// createMongodbLogsTable returns SQL for mongodb_logs table
func createMongodbLogsTable() string {
	return `
CREATE TABLE IF NOT EXISTS mongodb_logs (
	id serial4 NOT NULL,
	"timestamp" timestamptz NOT NULL,
	severity varchar(50) NULL,
	component varchar(50) NULL,
	context varchar(255) NULL,
	message text NULL,
	duration int4 NOT NULL,
	query_msg text NULL,
	log_file_name varchar(255) NULL,
	nodename varchar NULL,
	CONSTRAINT mongodb_logs_pkey PRIMARY KEY (id)
);`
}

// createMongoJsonDataTable returns SQL for mongojsondata table
func createMongoJsonDataTable() string {
	return `
CREATE TABLE IF NOT EXISTS mongojsondata (
	id serial4 NOT NULL,
	jsondata jsonb NOT NULL,
	replsetname varchar NULL,
	nodename varchar NULL,
	datetime timestamp DEFAULT now() NOT NULL,
	CONSTRAINT mongojsondata_pkey PRIMARY KEY (id)
);`
}

// createMssqlDataTable returns SQL for mssql_data table
func createMssqlDataTable() string {
	return `
CREATE TABLE IF NOT EXISTS mssql_data (
	id serial4 NOT NULL,
	jsondata jsonb NOT NULL,
	clustername varchar(255) NOT NULL,
	created_at timestamp DEFAULT CURRENT_TIMESTAMP NOT NULL,
	updated_at timestamp DEFAULT CURRENT_TIMESTAMP NOT NULL,
	CONSTRAINT mssql_data_pkey PRIMARY KEY (id),
	CONSTRAINT unique_mssql_clustername UNIQUE (clustername)
);
CREATE INDEX IF NOT EXISTS idx_mssql_data_clustername ON mssql_data USING btree (clustername);
CREATE INDEX IF NOT EXISTS idx_mssql_data_created_at ON mssql_data USING btree (created_at);`
}

// createNodeStatusTable returns SQL for node_status table
func createNodeStatusTable() string {
	return `
CREATE TABLE IF NOT EXISTS node_status (
	id serial4 NOT NULL,
	listener_name varchar(255) NOT NULL,
	node_name varchar(255) NOT NULL,
	node_status varchar(50) NULL,
	drive_letter varchar(10) NULL,
	free_space_gb numeric(10, 2) NULL,
	updated_at timestamp DEFAULT CURRENT_TIMESTAMP NULL,
	ag_status varchar(50) NULL,
	synchronization_health varchar(255) NULL,
	"version" varchar(255) NULL,
	total_space_gb numeric(10, 2) NULL,
	free_percentage float8 NULL,
	drive_name varchar(255) NULL,
	CONSTRAINT node_status_listener_name_node_name_drive_letter_key UNIQUE (listener_name, node_name, drive_letter),
	CONSTRAINT node_status_pkey PRIMARY KEY (id)
);`
}

// createNotificationSettingsTable returns SQL for notification_settings table
func createNotificationSettingsTable() string {
	return `
CREATE TABLE IF NOT EXISTS notification_settings (
	id serial4 NOT NULL,
	slack_webhook_url text NULL,
	slack_enabled bool DEFAULT false NULL,
	email_enabled bool DEFAULT false NULL,
	email_server text NULL,
	email_port text NULL,
	email_user text NULL,
	email_password text NULL,
	email_from text NULL,
	email_recipients _text NULL,
	created_at timestamptz DEFAULT CURRENT_TIMESTAMP NULL,
	updated_at timestamptz DEFAULT CURRENT_TIMESTAMP NULL,
	CONSTRAINT notification_settings_pkey PRIMARY KEY (id)
);`
}

// createOperationLogsTable returns SQL for operation_logs table
func createOperationLogsTable() string {
	return `
CREATE TABLE IF NOT EXISTS operation_logs (
	id serial4 NOT NULL,
	changedc_id int4 NOT NULL,
	log_message text NOT NULL,
	created_at timestamp DEFAULT CURRENT_TIMESTAMP NULL,
	CONSTRAINT operation_logs_pkey PRIMARY KEY (id)
);`
}

// createPostgresDataTable returns SQL for postgres_data table
func createPostgresDataTable() string {
	return `
CREATE TABLE IF NOT EXISTS postgres_data (
	id serial4 NOT NULL,
	jsondata jsonb NULL,
	clustername varchar NULL,
	created_at timestamp DEFAULT CURRENT_TIMESTAMP NOT NULL,
	updated_at timestamp DEFAULT CURRENT_TIMESTAMP NOT NULL,
	CONSTRAINT postgres_data_pkey PRIMARY KEY (id),
	CONSTRAINT unique_postgres_clustername UNIQUE (clustername)
);`
}

// createProcessLogsTable returns SQL for process_logs table
func createProcessLogsTable() string {
	return `
CREATE TABLE IF NOT EXISTS process_logs (
	id serial4 NOT NULL,
	process_id text NOT NULL,
	agent_id text NOT NULL,
	process_type text NOT NULL,
	status text NOT NULL,
	log_messages jsonb NOT NULL,
	elapsed_time_s float4 NULL,
	metadata jsonb NULL,
	created_at timestamp DEFAULT now() NOT NULL,
	updated_at timestamp DEFAULT now() NOT NULL,
	CONSTRAINT process_logs_pkey PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS idx_process_logs_agent_id ON process_logs USING btree (agent_id);
CREATE INDEX IF NOT EXISTS idx_process_logs_process_id ON process_logs USING btree (process_id);
CREATE INDEX IF NOT EXISTS idx_process_logs_process_type ON process_logs USING btree (process_type);
CREATE INDEX IF NOT EXISTS idx_process_logs_status ON process_logs USING btree (status);
CREATE INDEX IF NOT EXISTS idx_process_logs_updated_at ON process_logs USING btree (updated_at);`
}

// createRdsMssqlDataTable returns SQL for rds_mssql_data table
func createRdsMssqlDataTable() string {
	return `
CREATE TABLE IF NOT EXISTS rds_mssql_data (
	id serial4 NOT NULL,
	jsondata jsonb NOT NULL,
	clustername varchar(255) NOT NULL,
	region varchar(50) NOT NULL,
	aws_account_id varchar(50) NULL,
	created_at timestamp DEFAULT CURRENT_TIMESTAMP NULL,
	updated_at timestamp DEFAULT CURRENT_TIMESTAMP NULL,
	CONSTRAINT rds_mssql_data_pkey PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS idx_rds_mssql_data_aws_account ON rds_mssql_data USING btree (aws_account_id);
CREATE INDEX IF NOT EXISTS idx_rds_mssql_data_clustername ON rds_mssql_data USING btree (clustername);
CREATE INDEX IF NOT EXISTS idx_rds_mssql_data_created_at ON rds_mssql_data USING btree (created_at);
CREATE INDEX IF NOT EXISTS idx_rds_mssql_data_region ON rds_mssql_data USING btree (region);`
}

// createReplSetInfoTable returns SQL for replsetinfo table
func createReplSetInfoTable() string {
	return `
CREATE TABLE IF NOT EXISTS replsetinfo (
	id bigserial NOT NULL,
	replsetname varchar NULL,
	nodename varchar NULL,
	datetime timestamp DEFAULT now() NOT NULL,
	CONSTRAINT mongojsondata_pkey_1 PRIMARY KEY (id)
);
CREATE UNIQUE INDEX IF NOT EXISTS replsetinfo_nodename_idx ON replsetinfo USING btree (nodename);`
}

// createSettingsTable returns SQL for settings table
func createSettingsTable() string {
	return `
CREATE TABLE IF NOT EXISTS settings (
	id serial4 NOT NULL,
	"key" varchar(50) NOT NULL,
	value bool NOT NULL,
	CONSTRAINT settings_key_key UNIQUE (key),
	CONSTRAINT settings_pkey PRIMARY KEY (id)
);`
}

// createThresholdSettingsTable returns SQL for threshold_settings table
func createThresholdSettingsTable() string {
	return `
CREATE TABLE IF NOT EXISTS threshold_settings (
	id serial4 NOT NULL,
	cpu_threshold numeric(5, 2) NOT NULL,
	memory_threshold numeric(5, 2) NOT NULL,
	disk_threshold numeric(5, 2) NOT NULL,
	slow_query_threshold_ms int8 NOT NULL,
	connection_threshold int4 NOT NULL,
	replication_lag_threshold int4 NOT NULL,
	created_at timestamptz DEFAULT CURRENT_TIMESTAMP NULL,
	blocking_query_threshold_ms int8 NOT NULL,
	CONSTRAINT threshold_settings_pkey PRIMARY KEY (id)
);`
}

// createUsersTable returns SQL for users table
func createUsersTable() string {
	return `
CREATE TABLE IF NOT EXISTS users (
	id serial4 NOT NULL,
	username varchar NULL,
	password_hash varchar(60) NULL,
	email varchar NULL,
	status varchar NULL,
	"admin" bool NULL,
	created_at timestamptz DEFAULT now() NULL,
	updated_at timestamptz DEFAULT now() NULL,
	totp_secret varchar(64) NULL,
	totp_enabled bool DEFAULT false NULL,
	backup_codes _text NULL,
	CONSTRAINT errorlog_pkey_1_1_2 PRIMARY KEY (id)
);`
}