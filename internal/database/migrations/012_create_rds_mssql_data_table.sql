-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS rds_mssql_data (
    id SERIAL PRIMARY KEY,
    jsondata JSONB NOT NULL, -- AWS RDS MSSQL cluster and instance information stored in JSON format
    clustername VARCHAR(255) NOT NULL, -- RDS Cluster/Instance identifier
    region VARCHAR(50) NOT NULL, -- AWS Region
    aws_account_id VARCHAR(50), -- AWS Account ID (optional)
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_rds_mssql_data_clustername ON rds_mssql_data(clustername);
CREATE INDEX idx_rds_mssql_data_region ON rds_mssql_data(region);
CREATE INDEX idx_rds_mssql_data_created_at ON rds_mssql_data(created_at);
CREATE INDEX idx_rds_mssql_data_aws_account ON rds_mssql_data(aws_account_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS rds_mssql_data;
-- +goose StatementEnd