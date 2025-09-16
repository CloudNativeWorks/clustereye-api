package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sefaphlvn/clustereye-test/internal/logger"
	"github.com/sefaphlvn/clustereye-test/internal/server"
)

type ReportRequest struct {
	TimeRange struct {
		Type     string `json:"type"`     // "relative" | "absolute"
		Relative string `json:"relative"` // "1h" | "1d" | "7d" | "30d"
		Absolute struct {
			StartDate string `json:"startDate"`
			EndDate   string `json:"endDate"`
		} `json:"absolute"`
	} `json:"timeRange"`
	Clusters []string `json:"clusters"`
	Sections struct {
		SystemMetrics   bool `json:"systemMetrics"`
		DiskAnalysis    bool `json:"diskAnalysis"`
		MongoAnalysis   struct {
			Enabled               bool `json:"enabled"`
			IncludeCollections    bool `json:"includeCollections"`
			IncludeIndexes        bool `json:"includeIndexes"`
			IncludePerformance    bool `json:"includePerformance"`
		} `json:"mongoAnalysis"`
		PostgresAnalysis struct {
			Enabled            bool `json:"enabled"`
			IncludeBloat       bool `json:"includeBloat"`
			IncludeSlowQueries bool `json:"includeSlowQueries"`
			IncludeConnections bool `json:"includeConnections"`
		} `json:"postgresAnalysis"`
		MssqlAnalysis struct {
			Enabled                bool `json:"enabled"`
			IncludeCapacityPlanning bool `json:"includeCapacityPlanning"`
			IncludePerformance     bool `json:"includePerformance"`
		} `json:"mssqlAnalysis"`
		AlarmsAnalysis    bool `json:"alarmsAnalysis"`
		Recommendations   bool `json:"recommendations"`
	} `json:"sections"`
}


type ReportData struct {
	SystemMetrics    interface{} `json:"systemMetrics,omitempty"`
	DiskAnalysis     interface{} `json:"diskAnalysis,omitempty"`
	MongoAnalysis    interface{} `json:"mongoAnalysis,omitempty"`
	PostgresAnalysis interface{} `json:"postgresAnalysis,omitempty"`
	AlarmsData       interface{} `json:"alarmsData,omitempty"`
	CapacityPlanning interface{} `json:"capacityPlanning,omitempty"`
	RecentAlarms     interface{} `json:"recentAlarms,omitempty"`
}


func GenerateReport(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req ReportRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			logger.Error().Err(err).Msg("Report request bind error")
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
			return
		}

		logger.Info().
			Interface("clusters", req.Clusters).
			Interface("timeRange", req.TimeRange).
			Interface("sections", req.Sections).
			Msg("Report generation started")

		// Validate request
		if len(req.Clusters) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "At least one cluster must be selected"})
			return
		}

		// Convert time range to InfluxDB format
		timeRange, err := convertTimeRange(req.TimeRange)
		if err != nil {
			logger.Error().Err(err).Msg("Invalid time range")
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid time range"})
			return
		}

		reportData := &ReportData{}

		// Generate system metrics if requested
		if req.Sections.SystemMetrics {
			systemData, err := generateSystemMetrics(server, req.Clusters, timeRange)
			if err != nil {
				logger.Error().Err(err).Msg("Failed to generate system metrics")
				reportData.SystemMetrics = make(map[string]interface{})
			} else {
				reportData.SystemMetrics = systemData
			}
		}

		// Generate disk analysis if requested
		if req.Sections.DiskAnalysis {
			diskData, err := generateDiskAnalysis(server, req.Clusters, timeRange)
			if err != nil {
				logger.Error().Err(err).Msg("Failed to generate disk analysis")
				reportData.DiskAnalysis = make(map[string]interface{})
			} else {
				reportData.DiskAnalysis = diskData
			}
		}

		// Generate MongoDB analysis if requested
		if req.Sections.MongoAnalysis.Enabled {
			mongoData, err := generateMongoAnalysis(server, req.Clusters, timeRange, req.Sections.MongoAnalysis)
			if err != nil {
				logger.Error().Err(err).Msg("Failed to generate MongoDB analysis")
				reportData.MongoAnalysis = make(map[string]interface{})
			} else {
				reportData.MongoAnalysis = mongoData
			}
		}

		// Generate PostgreSQL analysis if requested
		if req.Sections.PostgresAnalysis.Enabled {
			pgData, err := generatePostgresAnalysis(server, req.Clusters, timeRange, req.Sections.PostgresAnalysis)
			if err != nil {
				logger.Error().Err(err).Msg("Failed to generate PostgreSQL analysis")
				reportData.PostgresAnalysis = make(map[string]interface{})
			} else {
				reportData.PostgresAnalysis = pgData
			}
		}

		// Generate alarms analysis if requested
		if req.Sections.AlarmsAnalysis {
			alarmsData, err := generateAlarmsAnalysis(server, req.Clusters, timeRange)
			if err != nil {
				logger.Error().Err(err).Msg("Failed to generate alarms analysis")
				reportData.AlarmsData = make(map[string]interface{})
			} else {
				reportData.AlarmsData = alarmsData
			}
		}

		// Generate capacity planning and recommendations if requested
		if req.Sections.Recommendations {
			capacityData, err := generateCapacityPlanning(server, req.Clusters, timeRange)
			if err != nil {
				logger.Error().Err(err).Msg("Failed to generate capacity planning")
				reportData.CapacityPlanning = make(map[string]interface{})
			} else {
				reportData.CapacityPlanning = capacityData
			}
		}
		
		// Always get recent alarms for reports
		recentAlarms, err := generateRecentAlarms(server)
		if err != nil {
			logger.Error().Err(err).Msg("Failed to generate recent alarms")
			reportData.RecentAlarms = make([]interface{}, 0)
		} else {
			reportData.RecentAlarms = recentAlarms
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data":    reportData,
		})
	}
}

func convertTimeRange(timeRange struct {
	Type     string `json:"type"`
	Relative string `json:"relative"`
	Absolute struct {
		StartDate string `json:"startDate"`
		EndDate   string `json:"endDate"`
	} `json:"absolute"`
}) (string, error) {
	if timeRange.Type == "relative" {
		return timeRange.Relative, nil
	} else if timeRange.Type == "absolute" {
		// For absolute time ranges, we calculate the difference
		start, err := time.Parse(time.RFC3339, timeRange.Absolute.StartDate)
		if err != nil {
			return "", fmt.Errorf("invalid start date: %w", err)
		}
		end, err := time.Parse(time.RFC3339, timeRange.Absolute.EndDate)
		if err != nil {
			return "", fmt.Errorf("invalid end date: %w", err)
		}
		
		duration := end.Sub(start)
		hours := int(duration.Hours())
		if hours < 1 {
			return "1h", nil
		}
		return fmt.Sprintf("%dh", hours), nil
	}
	
	return "", fmt.Errorf("invalid time range type: %s", timeRange.Type)
}

func generateSystemMetrics(server *server.Server, clusters []string, timeRange string) (interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	logger.Info().Strs("clusters", clusters).Str("timeRange", timeRange).Msg("Generating system metrics")
	
	influxWriter := server.GetInfluxWriter()
	if influxWriter == nil {
		return nil, fmt.Errorf("InfluxDB service unavailable")
	}
	
	// Query all system metrics (CPU, Memory, Disk)
	clustersList := `"` + strings.Join(clusters, `", "`) + `"`
	systemQuery := fmt.Sprintf(`from(bucket: "clustereye") 
		|> range(start: -%s) 
		|> filter(fn: (r) => r._measurement == "mongodb_system" or r._measurement == "postgresql_system" or r._measurement == "mssql_system") 
		|> filter(fn: (r) => r._field == "cpu_usage" or r._field == "memory_usage" or r._field == "free_disk" or r._field == "total_disk" or r._field == "free_memory" or r._field == "total_memory")
		|> filter(fn: (r) => contains(value: r.agent_id, set: [%s])) 
		|> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, clustersList)
	
	logger.Info().Str("query", systemQuery).Msg("Executing system metrics query")
	
	result, err := influxWriter.QueryMetrics(ctx, systemQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to query system metrics: %w", err)
	}
	
	logger.Info().Int("records", len(result)).Msg("System metrics query result")
	return result, nil
}

func generateDiskAnalysis(server *server.Server, clusters []string, timeRange string) (interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	influxWriter := server.GetInfluxWriter()
	if influxWriter == nil {
		return nil, fmt.Errorf("InfluxDB service unavailable")
	}
	
	// Query disk metrics and details
	clustersList := `"` + strings.Join(clusters, `", "`) + `"`
	diskQuery := fmt.Sprintf(`from(bucket: "clustereye") 
		|> range(start: -%s) 
		|> filter(fn: (r) => r._measurement == "disk_usage") 
		|> filter(fn: (r) => contains(value: r.agent_id, set: [%s])) 
		|> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, clustersList)
	
	result, err := influxWriter.QueryMetrics(ctx, diskQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to query disk analysis: %w", err)
	}
	
	return result, nil
}

func generateMongoAnalysis(server *server.Server, clusters []string, timeRange string, config struct {
	Enabled               bool `json:"enabled"`
	IncludeCollections    bool `json:"includeCollections"`
	IncludeIndexes        bool `json:"includeIndexes"`
	IncludePerformance    bool `json:"includePerformance"`
}) (interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	results := make(map[string]interface{})
	
	// For each cluster, get real-time MongoDB data via agent query
	for _, agentID := range clusters {
		agentResults := make(map[string]interface{})
		
		if config.IncludeCollections {
			// Get collection stats from existing API endpoint
			collections, err := server.SendQuery(ctx, agentID, "list_databases", "show dbs", "")
			if err != nil {
				logger.Error().Err(err).Str("agent_id", agentID).Msg("Failed to get databases")
			} else {
				agentResults["collections"] = collections.Result
			}
		}
		
		if config.IncludeIndexes {
			// Get index usage stats (real-time)
			indexUsageQuery := `db.adminCommand("listCollections").cursor.firstBatch.forEach(function(collection) {
				db.getCollection(collection.name).getIndexes().forEach(function(index) {
					var stats = db.getCollection(collection.name).aggregate([
						{$indexStats: {}},
						{$match: {"name": index.name}}
					]).toArray();
					if (stats.length > 0) {
						print(JSON.stringify({
							collection: collection.name,
							name: index.name,
							keys: Object.keys(index.key),
							accesses: stats[0].accesses || {ops: 0, since: new Date()},
							size: Math.round((stats[0].spec ? 0 : 0) / 1024 / 1024 * 100) / 100
						}));
					}
				});
			});`
			
			indexUsage, err := server.SendQuery(ctx, agentID, "index_usage", indexUsageQuery, "")
			if err != nil {
				logger.Error().Err(err).Str("agent_id", agentID).Msg("Failed to get index usage")
			} else {
				agentResults["indexUsage"] = indexUsage.Result
			}
		}
		
		if config.IncludePerformance {
			// Get performance stats (real-time)
			performanceQuery := `var status = db.serverStatus();
			print(JSON.stringify({
				opcounters: status.opcounters,
				connections: status.connections,
				memory: status.mem,
				network: status.network,
				metrics: status.metrics,
				uptime: status.uptime
			}));`
			
			performance, err := server.SendQuery(ctx, agentID, "performance", performanceQuery, "")
			if err != nil {
				logger.Error().Err(err).Str("agent_id", agentID).Msg("Failed to get performance stats")
			} else {
				agentResults["performance"] = performance.Result
			}
		}
		
		if len(agentResults) > 0 {
			results[agentID] = agentResults
		}
	}
	
	return results, nil
}

func generatePostgresAnalysis(server *server.Server, clusters []string, timeRange string, config struct {
	Enabled            bool `json:"enabled"`
	IncludeBloat       bool `json:"includeBloat"`
	IncludeSlowQueries bool `json:"includeSlowQueries"`
	IncludeConnections bool `json:"includeConnections"`
}) (interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	results := make(map[string]interface{})
	
	// For each cluster, get real-time PostgreSQL data via agent query
	for _, agentID := range clusters {
		agentResults := make(map[string]interface{})
		
		if config.IncludeBloat {
			// Get table bloat information
			tableBloatQuery := `
				SELECT 
					schemaname, relname as tablename,
					pg_size_pretty(pg_total_relation_size(C.oid)) AS size
				FROM pg_class C
				LEFT JOIN pg_namespace N ON (N.oid = C.relnamespace)
				WHERE nspname NOT IN ('information_schema', 'pg_catalog', 'pg_toast')
					AND C.relkind = 'r'
				ORDER BY pg_total_relation_size(C.oid) DESC
				LIMIT 20;`
			
			tableBloat, err := server.SendQuery(ctx, agentID, "table_bloat", tableBloatQuery, "")
			if err != nil {
				logger.Error().Err(err).Str("agent_id", agentID).Msg("Failed to get table bloat")
			} else {
				agentResults["tableBloat"] = tableBloat.Result
			}
			
			// Get index bloat information
			indexBloatQuery := `
				SELECT schemaname, tablename, indexname,
					CASE WHEN ipages < 2 THEN 0.0 ELSE 
						CASE WHEN ipages <= approx_pages THEN 0.0 
						ELSE (ipages-approx_pages)::numeric / ipages END 
					END AS bloat_ratio,
					CASE WHEN ipages < approx_pages THEN 0 ELSE ipages::bigint - approx_pages END AS bloat_pages,
					CASE WHEN ipages < approx_pages THEN 0 ELSE bs*(ipages-approx_pages)::bigint END AS waste_bytes
				FROM (
					SELECT schemaname, tablename, indexname, indtuples, bs, ipages,
						CEIL((indtuples*(20+nullfrac*7))::numeric/bs)::bigint AS approx_pages
					FROM (
						SELECT schemaname, tablename, indexname, bs, ipages, indtuples,
							(100-pg_stats.null_frac*100) / 100 AS nullfrac
						FROM pg_stat_user_indexes psi
						JOIN pg_class i ON i.oid = psi.indexrelid
						JOIN pg_class t ON t.oid = psi.relid
						JOIN pg_namespace n ON n.oid = t.relnamespace
						JOIN pg_stats ON schemaname = n.nspname AND tablename = t.relname AND attname = pg_get_indexdef(i.oid)
						JOIN (SELECT 8192 as bs) bs ON true
						WHERE i.relpages > 128
					) AS ss
				) AS rs
				WHERE ipages - approx_pages > 128
				ORDER BY waste_bytes DESC LIMIT 20;`
				
			indexBloat, err := server.SendQuery(ctx, agentID, "index_bloat", indexBloatQuery, "")
			if err != nil {
				logger.Error().Err(err).Str("agent_id", agentID).Msg("Failed to get index bloat")
			} else {
				agentResults["indexBloat"] = indexBloat.Result
			}
		}
		
		if config.IncludeSlowQueries {
			// Get top queries (real-time)
			topQueriesQuery := `
				SELECT 
					query,
					state,
					query_start,
					state_change,
					usename,
					application_name,
					client_addr
				FROM pg_stat_activity 
				WHERE state != 'idle' 
					AND query != ''
					AND pid != pg_backend_pid()
				ORDER BY query_start DESC 
				LIMIT 10;`
				
			topQueries, err := server.SendQuery(ctx, agentID, "top_queries", topQueriesQuery, "")
			if err != nil {
				logger.Error().Err(err).Str("agent_id", agentID).Msg("Failed to get top queries")
			} else {
				agentResults["topQueries"] = topQueries.Result
			}
		}
		
		if config.IncludeConnections {
			// Get connection statistics (real-time)
			connectionsQuery := `
				SELECT 
					state,
					COUNT(*) as count,
					MAX(EXTRACT(epoch FROM (now() - state_change))) as longest_duration
				FROM pg_stat_activity 
				WHERE state IS NOT NULL 
				GROUP BY state
				ORDER BY count DESC;`
				
			connections, err := server.SendQuery(ctx, agentID, "connection_stats", connectionsQuery, "")
			if err != nil {
				logger.Error().Err(err).Str("agent_id", agentID).Msg("Failed to get connection stats")
			} else {
				agentResults["connectionStats"] = connections.Result
			}
		}
		
		if len(agentResults) > 0 {
			results[agentID] = agentResults
		}
	}
	
	return results, nil
}

func generateAlarmsAnalysis(server *server.Server, clusters []string, timeRange string) (interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	influxWriter := server.GetInfluxWriter()
	if influxWriter == nil {
		return nil, fmt.Errorf("InfluxDB service unavailable")
	}
	
	clustersList := `"` + strings.Join(clusters, `", "`) + `"`
	
	alarmsQuery := fmt.Sprintf(`from(bucket: "clustereye") 
		|> range(start: -%s) 
		|> filter(fn: (r) => r._measurement == "mongodb_system" or r._measurement == "postgresql_system" or r._measurement == "mssql_system") 
		|> filter(fn: (r) => contains(value: r.agent_id, set: [%s]))`, timeRange, clustersList)
	
	result, err := influxWriter.QueryMetrics(ctx, alarmsQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to query alarms: %w", err)
	}
	
	return result, nil
}

func generateCapacityPlanning(server *server.Server, clusters []string, timeRange string) (interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	influxWriter := server.GetInfluxWriter()
	if influxWriter == nil {
		return nil, fmt.Errorf("InfluxDB service unavailable")
	}
	
	clustersList := `"` + strings.Join(clusters, `", "`) + `"`
	results := make(map[string]interface{})
	
	// Query disk growth trends for capacity planning
	diskTrendsQuery := fmt.Sprintf(`from(bucket: "clustereye") 
		|> range(start: -%s) 
		|> filter(fn: (r) => r._measurement == "mongodb_system" or r._measurement == "postgresql_system" or r._measurement == "mssql_system")
		|> filter(fn: (r) => r._field == "free_disk" or r._field == "total_disk")
		|> filter(fn: (r) => contains(value: r.agent_id, set: [%s])) 
		|> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, clustersList)
	
	diskTrends, err := influxWriter.QueryMetrics(ctx, diskTrendsQuery)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to query disk trends for capacity planning")
	} else {
		results["diskTrends"] = diskTrends
	}
	
	// Query memory utilization trends
	memoryTrendsQuery := fmt.Sprintf(`from(bucket: "clustereye") 
		|> range(start: -%s) 
		|> filter(fn: (r) => r._measurement == "mongodb_system" or r._measurement == "postgresql_system" or r._measurement == "mssql_system")
		|> filter(fn: (r) => r._field == "memory_usage" or r._field == "free_memory" or r._field == "total_memory")
		|> filter(fn: (r) => contains(value: r.agent_id, set: [%s])) 
		|> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, clustersList)
	
	memoryTrends, err := influxWriter.QueryMetrics(ctx, memoryTrendsQuery)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to query memory trends for capacity planning")
	} else {
		results["memoryTrends"] = memoryTrends
	}
	
	// Query CPU utilization trends  
	cpuTrendsQuery := fmt.Sprintf(`from(bucket: "clustereye") 
		|> range(start: -%s) 
		|> filter(fn: (r) => r._measurement == "mongodb_system" or r._measurement == "postgresql_system" or r._measurement == "mssql_system")
		|> filter(fn: (r) => r._field == "cpu_usage")
		|> filter(fn: (r) => contains(value: r.agent_id, set: [%s])) 
		|> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, clustersList)
	
	cpuTrends, err := influxWriter.QueryMetrics(ctx, cpuTrendsQuery)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to query CPU trends for capacity planning")
	} else {
		results["cpuTrends"] = cpuTrends
	}
	
	// Query connection usage trends for databases
	connectionTrendsQuery := fmt.Sprintf(`from(bucket: "clustereye") 
		|> range(start: -%s) 
		|> filter(fn: (r) => r._measurement == "mongodb_connections" or r._measurement == "postgresql_connections" or r._measurement == "mssql_connections") 
		|> filter(fn: (r) => r._field == "current" or r._field == "available" or r._field == "total" or r._field == "max_connections") 
		|> filter(fn: (r) => contains(value: r.agent_id, set: [%s])) 
		|> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, clustersList)
	
	connectionTrends, err := influxWriter.QueryMetrics(ctx, connectionTrendsQuery)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to query connection trends for capacity planning")
	} else {
		results["connectionTrends"] = connectionTrends
	}
	
	// Query database size trends for storage capacity planning
	dbSizeTrendsQuery := fmt.Sprintf(`from(bucket: "clustereye") 
		|> range(start: -%s) 
		|> filter(fn: (r) => r._measurement == "mongodb_memory" or r._measurement == "postgresql_database" or r._measurement == "mssql_system") 
		|> filter(fn: (r) => contains(value: r.agent_id, set: [%s])) 
		|> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, clustersList)
	
	dbSizeTrends, err := influxWriter.QueryMetrics(ctx, dbSizeTrendsQuery)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to query database size trends for capacity planning")
	} else {
		results["databaseSizeTrends"] = dbSizeTrends
	}
	
	return results, nil
}

func generateRecentAlarms(server *server.Server) (interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	db := server.GetDB()
	if db == nil {
		return nil, fmt.Errorf("Database unavailable")
	}
	
	// Get recent alarms (last 5)
	query := `
		SELECT event_id, metric_name, agent_id, severity, message, 
		       value, threshold, created_at, acknowledged_at
		FROM alarms 
		WHERE acknowledged_at IS NULL 
		ORDER BY created_at DESC 
		LIMIT 5`
	
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query recent alarms: %w", err)
	}
	defer rows.Close()
	
	var alarms []map[string]interface{}
	for rows.Next() {
		var alarm map[string]interface{} = make(map[string]interface{})
		var eventID, metricName, agentID, severity, message string
		var value, threshold float64
		var createdAt time.Time
		var acknowledgedAt *time.Time
		
		err := rows.Scan(&eventID, &metricName, &agentID, &severity, &message, 
						&value, &threshold, &createdAt, &acknowledgedAt)
		if err != nil {
			continue
		}
		
		alarm["event_id"] = eventID
		alarm["metric_name"] = metricName
		alarm["agent_id"] = agentID
		alarm["severity"] = severity
		alarm["message"] = message
		alarm["value"] = value
		alarm["threshold"] = threshold
		alarm["created_at"] = createdAt
		alarm["acknowledged_at"] = acknowledgedAt
		
		alarms = append(alarms, alarm)
	}
	
	return alarms, nil
}

// Debug endpoint to check available data in InfluxDB
func DebugInfluxData(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		
		influxWriter := server.GetInfluxWriter()
		if influxWriter == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "InfluxDB service unavailable"})
			return
		}
		
		result := make(map[string]interface{})
		
		// Test single measurement with one agent
		testQuery := `from(bucket: "clustereye") 
			|> range(start: -1h) 
			|> filter(fn: (r) => r._measurement == "disk_usage") 
			|> filter(fn: (r) => r.agent_id == "agent_fenrir") 
			|> limit(n: 5)`
		
		testData, err := influxWriter.QueryMetrics(ctx, testQuery)
		if err != nil {
			result["test_error"] = err.Error()
		} else {
			result["testData"] = testData
			result["testCount"] = len(testData)
		}
		
		// Get connected agents
		connectedAgents := server.GetConnectedAgents()
		result["connectedAgents"] = len(connectedAgents)
		
		c.JSON(http.StatusOK, result)
	}
}

