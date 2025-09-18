package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/CloudNativeWorks/clustereye-api/internal/server"
)

// MongoDB System Metrics Handlers

// getMongoDBSystemCPUMetrics, MongoDB sistem CPU metriklerini getirir
func getMongoDBSystemCPUMetrics(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := c.Query("agent_id")
		timeRange := c.DefaultQuery("range", "1h")

		var query string
		if agentID != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_system") |> filter(fn: (r) => r._field == "cpu_usage" or r._field == "cpu_cores") |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, regexp.QuoteMeta(agentID))
		} else {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_system") |> filter(fn: (r) => r._field == "cpu_usage" or r._field == "cpu_cores") |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange)
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		influxWriter := server.GetInfluxWriter()
		if influxWriter == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "error",
				"error":  "InfluxDB servisi kullanılamıyor",
			})
			return
		}

		results, err := influxWriter.QueryMetrics(ctx, query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "MongoDB sistem CPU metrikleri alınamadı: " + err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   results,
		})
	}
}

// getMongoDBSystemMemoryMetrics, MongoDB sistem memory metriklerini getirir
func getMongoDBSystemMemoryMetrics(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := c.Query("agent_id")
		timeRange := c.DefaultQuery("range", "1h")

		var query string
		if agentID != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_system") |> filter(fn: (r) => r._field == "memory_usage" or r._field == "total_memory" or r._field == "free_memory") |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, regexp.QuoteMeta(agentID))
		} else {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_system") |> filter(fn: (r) => r._field == "memory_usage" or r._field == "total_memory" or r._field == "free_memory") |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange)
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		influxWriter := server.GetInfluxWriter()
		if influxWriter == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "error",
				"error":  "InfluxDB servisi kullanılamıyor",
			})
			return
		}

		results, err := influxWriter.QueryMetrics(ctx, query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "MongoDB sistem memory metrikleri alınamadı: " + err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   results,
		})
	}
}

// getMongoDBSystemDiskMetrics, MongoDB sistem disk metriklerini getirir
func getMongoDBSystemDiskMetrics(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := c.Query("agent_id")
		timeRange := c.DefaultQuery("range", "1h")

		var query string
		if agentID != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_system") |> filter(fn: (r) => r._field == "total_disk" or r._field == "free_disk" or r._field == "usage_percent" or r._field == "value") |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> aggregateWindow(every: 5m, fn: last, createEmpty: false)`, timeRange, regexp.QuoteMeta(agentID))
		} else {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_system") |> filter(fn: (r) => r._field == "total_disk" or r._field == "free_disk" or r._field == "usage_percent" or r._field == "value") |> aggregateWindow(every: 5m, fn: last, createEmpty: false)`, timeRange)
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		influxWriter := server.GetInfluxWriter()
		if influxWriter == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "error",
				"error":  "InfluxDB servisi kullanılamıyor",
			})
			return
		}

		results, err := influxWriter.QueryMetrics(ctx, query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "MongoDB sistem disk metrikleri alınamadı: " + err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   results,
		})
	}
}

// getMongoDBSystemResponseTimeMetrics, MongoDB sistem response time metriklerini getirir
func getMongoDBSystemResponseTimeMetrics(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := c.Query("agent_id")
		timeRange := c.DefaultQuery("range", "1h")

		var query string
		if agentID != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_system") |> filter(fn: (r) => r._field == "response_time_ms") |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> aggregateWindow(every: 1m, fn: mean, createEmpty: false) |> sort(columns: ["_time"], desc: false)`, timeRange, regexp.QuoteMeta(agentID))
		} else {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_system") |> filter(fn: (r) => r._field == "response_time_ms") |> aggregateWindow(every: 1m, fn: mean, createEmpty: false) |> sort(columns: ["_time"], desc: false)`, timeRange)
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		influxWriter := server.GetInfluxWriter()
		if influxWriter == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "error",
				"error":  "InfluxDB servisi kullanılamıyor",
			})
			return
		}

		results, err := influxWriter.QueryMetrics(ctx, query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "MongoDB response time metrikleri alınamadı: " + err.Error(),
			})
			return
		}

		// Response time istatistikleri hesapla
		var totalResponseTime, minResponseTime, maxResponseTime, avgResponseTime float64
		var responseTimeCount int

		if len(results) > 0 {
			minResponseTime = 99999999 // Büyük bir başlangıç değeri
			maxResponseTime = 0

			for _, result := range results {
				if val, ok := result["_value"]; ok {
					if responseTime, ok := val.(float64); ok && responseTime > 0 {
						totalResponseTime += responseTime
						responseTimeCount++

						if responseTime < minResponseTime {
							minResponseTime = responseTime
						}
						if responseTime > maxResponseTime {
							maxResponseTime = responseTime
						}
					}
				}
			}

			if responseTimeCount > 0 {
				avgResponseTime = totalResponseTime / float64(responseTimeCount)
			}

			// Eğer hiç veri yoksa minimum değeri sıfırlayalım
			if responseTimeCount == 0 {
				minResponseTime = 0
			}
		}

		// Latest response time'ı al
		var latestResponseTime float64
		var latestTimestamp string
		if len(results) > 0 {
			lastResult := results[len(results)-1]
			if val, ok := lastResult["_value"]; ok {
				if responseTime, ok := val.(float64); ok {
					latestResponseTime = responseTime
				}
			}
			if timestamp, ok := lastResult["_time"]; ok {
				if timeStr, ok := timestamp.(string); ok {
					latestTimestamp = timeStr
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data": gin.H{
				"agent_id": agentID,
				"summary": gin.H{
					"latest_response_time_ms": latestResponseTime,
					"latest_timestamp":        latestTimestamp,
					"avg_response_time_ms":    avgResponseTime,
					"min_response_time_ms":    minResponseTime,
					"max_response_time_ms":    maxResponseTime,
					"total_measurements":      responseTimeCount,
				},
				"all_data": results,
			},
		})
	}
}

// MongoDB Database Metrics Handlers

// getMongoDBConnectionsMetrics, MongoDB bağlantı metriklerini getirir
func getMongoDBConnectionsMetrics(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := c.Query("agent_id")
		timeRange := c.DefaultQuery("range", "1h")

		var query string
		if agentID != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_connections") |> filter(fn: (r) => r._field == "current" or r._field == "available" or r._field == "total_created_connections") |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, regexp.QuoteMeta(agentID))
		} else {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_connections") |> filter(fn: (r) => r._field == "current" or r._field == "available" or r._field == "total_created_connections") |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange)
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		influxWriter := server.GetInfluxWriter()
		if influxWriter == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "error",
				"error":  "InfluxDB servisi kullanılamıyor",
			})
			return
		}

		results, err := influxWriter.QueryMetrics(ctx, query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "MongoDB bağlantı metrikleri alınamadı: " + err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   results,
		})
	}
}

// getMongoDBOperationsMetrics, MongoDB operasyon metriklerini getirir
func getMongoDBOperationsMetrics(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := c.Query("agent_id")
		database := c.Query("database") // İsteğe bağlı database filtresi
		timeRange := c.DefaultQuery("range", "1h")

		var query string
		operationFields := "r._field == \"insert\" or r._field == \"query\" or r._field == \"update\" or r._field == \"delete\" or r._field == \"getmore\" or r._field == \"command\""

		if agentID != "" && database != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_operations") |> filter(fn: (r) => %s) |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> filter(fn: (r) => r.database_name =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, operationFields, regexp.QuoteMeta(agentID), regexp.QuoteMeta(database))
		} else if agentID != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_operations") |> filter(fn: (r) => %s) |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, operationFields, regexp.QuoteMeta(agentID))
		} else {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_operations") |> filter(fn: (r) => %s) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, operationFields)
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		influxWriter := server.GetInfluxWriter()
		if influxWriter == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "error",
				"error":  "InfluxDB servisi kullanılamıyor",
			})
			return
		}

		results, err := influxWriter.QueryMetrics(ctx, query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "MongoDB operasyon metrikleri alınamadı: " + err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   results,
		})
	}
}

// getMongoDBOperationsRateMetrics, MongoDB operasyon rate metriklerini getirir (ops/sec)
func getMongoDBOperationsRateMetrics(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := c.Query("agent_id")
		database := c.Query("database") // İsteğe bağlı database filtresi
		timeRange := c.DefaultQuery("range", "1h")
		windowInterval := c.DefaultQuery("window", "1m") // Rate hesaplama penceresi

		var query string
		operationFields := "r._field == \"insert\" or r._field == \"query\" or r._field == \"update\" or r._field == \"delete\" or r._field == \"getmore\" or r._field == \"command\""

		if agentID != "" && database != "" {
			query = fmt.Sprintf(`
				from(bucket: "clustereye")
				|> range(start: -%s)
				|> filter(fn: (r) => r._measurement == "mongodb_operations")
				|> filter(fn: (r) => %s)
				|> filter(fn: (r) => r.agent_id =~ /^%s$/)
				|> filter(fn: (r) => r.database_name =~ /^%s$/)
				|> sort(columns: ["_time"])
				|> derivative(unit: 1s, nonNegative: true)
				|> aggregateWindow(every: %s, fn: mean, createEmpty: false)
			`, timeRange, operationFields, regexp.QuoteMeta(agentID), regexp.QuoteMeta(database), windowInterval)
		} else if agentID != "" {
			query = fmt.Sprintf(`
				from(bucket: "clustereye")
				|> range(start: -%s)
				|> filter(fn: (r) => r._measurement == "mongodb_operations")
				|> filter(fn: (r) => %s)
				|> filter(fn: (r) => r.agent_id =~ /^%s$/)
				|> sort(columns: ["_time"])
				|> derivative(unit: 1s, nonNegative: true)
				|> aggregateWindow(every: %s, fn: mean, createEmpty: false)
			`, timeRange, operationFields, regexp.QuoteMeta(agentID), windowInterval)
		} else {
			query = fmt.Sprintf(`
				from(bucket: "clustereye")
				|> range(start: -%s)
				|> filter(fn: (r) => r._measurement == "mongodb_operations")
				|> filter(fn: (r) => %s)
				|> sort(columns: ["_time"])
				|> derivative(unit: 1s, nonNegative: true)
				|> aggregateWindow(every: %s, fn: mean, createEmpty: false)
			`, timeRange, operationFields, windowInterval)
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		influxWriter := server.GetInfluxWriter()
		if influxWriter == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "error",
				"error":  "InfluxDB servisi kullanılamıyor",
			})
			return
		}

		results, err := influxWriter.QueryMetrics(ctx, query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "MongoDB operasyon rate metrikleri alınamadı: " + err.Error(),
			})
			return
		}

		// Rate istatistikleri hesapla
		rateStats := calculateOperationRateStats(results, agentID)

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data": gin.H{
				"agent_id": agentID,
				"summary":  rateStats,
				"all_data": results,
				"note":     "Rates are calculated as operations per second using derivative function",
			},
		})
	}
}

// calculateOperationRateStats, operasyon rate istatistiklerini hesaplar
func calculateOperationRateStats(results []map[string]interface{}, agentID string) map[string]interface{} {
	stats := make(map[string]interface{})

	// Field'lara göre grupla
	fieldStats := make(map[string][]float64)
	var latestTime time.Time
	latestValues := make(map[string]float64)
	var latestTimestamp string

	for _, result := range results {
		if field, ok := result["_field"].(string); ok {
			if val, ok := result["_value"]; ok {
				if rate, ok := val.(float64); ok && rate >= 0 {
					fieldStats[field] = append(fieldStats[field], rate)

					// En son değerleri bul
					if timestamp, ok := result["_time"]; ok {
						if timeStr, ok := timestamp.(string); ok {
							if parsedTime, err := time.Parse(time.RFC3339, timeStr); err == nil {
								if parsedTime.After(latestTime) {
									latestTime = parsedTime
									latestValues[field] = rate
									latestTimestamp = timeStr
								}
							}
						}
					}
				}
			}
		}
	}

	// Her field için istatistik hesapla
	for field, values := range fieldStats {
		if len(values) > 0 {
			var sum, min, max float64
			min = values[0]
			max = values[0]

			for _, val := range values {
				sum += val
				if val < min {
					min = val
				}
				if val > max {
					max = val
				}
			}

			avg := sum / float64(len(values))

			stats[field+"_ops_per_sec"] = map[string]interface{}{
				"latest":      latestValues[field],
				"avg":         avg,
				"min":         min,
				"max":         max,
				"data_points": len(values),
			}
		}
	}

	// Toplam operasyon rate'i hesapla
	var totalLatestRate float64
	for _, rate := range latestValues {
		totalLatestRate += rate
	}

	stats["total_ops_per_sec"] = totalLatestRate
	stats["latest_timestamp"] = latestTimestamp
	stats["operations_tracked"] = len(fieldStats)

	return stats
}

// getMongoDBStorageMetrics, MongoDB storage metriklerini getirir
func getMongoDBStorageMetrics(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := c.Query("agent_id")
		database := c.Query("database") // İsteğe bağlı database filtresi
		timeRange := c.DefaultQuery("range", "1h")

		var query string
		storageFields := "r._field == \"data_size_mb\" or r._field == \"storage_size_mb\" or r._field == \"index_size_mb\" or r._field == \"avg_obj_size\" or r._field == \"file_size_mb\""

		if agentID != "" && database != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_storage") |> filter(fn: (r) => %s) |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> filter(fn: (r) => r.database_name =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, storageFields, regexp.QuoteMeta(agentID), regexp.QuoteMeta(database))
		} else if agentID != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_storage") |> filter(fn: (r) => %s) |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, storageFields, regexp.QuoteMeta(agentID))
		} else {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_storage") |> filter(fn: (r) => %s) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, storageFields)
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		influxWriter := server.GetInfluxWriter()
		if influxWriter == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "error",
				"error":  "InfluxDB servisi kullanılamıyor",
			})
			return
		}

		results, err := influxWriter.QueryMetrics(ctx, query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "MongoDB storage metrikleri alınamadı: " + err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   results,
		})
	}
}

// getMongoDBDatabaseInfoMetrics, MongoDB database info metriklerini getirir
func getMongoDBDatabaseInfoMetrics(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := c.Query("agent_id")
		database := c.Query("database") // İsteğe bağlı database filtresi
		timeRange := c.DefaultQuery("range", "1h")

		var query string
		if agentID != "" && database != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_database") |> filter(fn: (r) => r._field == "database_count" or r._field == "collection_count") |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> filter(fn: (r) => r.database_name =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, regexp.QuoteMeta(agentID), regexp.QuoteMeta(database))
		} else if agentID != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_database") |> filter(fn: (r) => r._field == "database_count" or r._field == "collection_count") |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, regexp.QuoteMeta(agentID))
		} else {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_database") |> filter(fn: (r) => r._field == "database_count" or r._field == "collection_count") |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange)
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		influxWriter := server.GetInfluxWriter()
		if influxWriter == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "error",
				"error":  "InfluxDB servisi kullanılamıyor",
			})
			return
		}

		results, err := influxWriter.QueryMetrics(ctx, query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "MongoDB database info metrikleri alınamadı: " + err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   results,
		})
	}
}

// MongoDB Replication Metrics Handlers

// getMongoDBReplicationStatusMetrics, MongoDB replication durum metriklerini getirir
func getMongoDBReplicationStatusMetrics(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := c.Query("agent_id")
		replicaSetName := c.Query("replica_set") // İsteğe bağlı replica set filtresi
		nodeRole := c.Query("node_role")         // İsteğe bağlı node role filtresi
		timeRange := c.DefaultQuery("range", "1h")

		var query string
		if agentID != "" && replicaSetName != "" && nodeRole != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_replication") |> filter(fn: (r) => r._field == "member_state" or r._field == "members_healthy" or r._field == "members_total") |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> filter(fn: (r) => r.replica_set_name =~ /^%s$/) |> filter(fn: (r) => r.node_role =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, regexp.QuoteMeta(agentID), regexp.QuoteMeta(replicaSetName), regexp.QuoteMeta(nodeRole))
		} else if agentID != "" && replicaSetName != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_replication") |> filter(fn: (r) => r._field == "member_state" or r._field == "members_healthy" or r._field == "members_total") |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> filter(fn: (r) => r.replica_set_name =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, regexp.QuoteMeta(agentID), regexp.QuoteMeta(replicaSetName))
		} else if agentID != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_replication") |> filter(fn: (r) => r._field == "member_state" or r._field == "members_healthy" or r._field == "members_total") |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, regexp.QuoteMeta(agentID))
		} else {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_replication") |> filter(fn: (r) => r._field == "member_state" or r._field == "members_healthy" or r._field == "members_total") |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange)
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		influxWriter := server.GetInfluxWriter()
		if influxWriter == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "error",
				"error":  "InfluxDB servisi kullanılamıyor",
			})
			return
		}

		results, err := influxWriter.QueryMetrics(ctx, query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "MongoDB replication durum metrikleri alınamadı: " + err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   results,
		})
	}
}

// getMongoDBReplicationLagMetrics, MongoDB replication lag metriklerini getirir
func getMongoDBReplicationLagMetrics(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := c.Query("agent_id")
		replicaSetName := c.Query("replica_set") // İsteğe bağlı replica set filtresi
		timeRange := c.DefaultQuery("range", "1h")

		var query string
		if agentID != "" && replicaSetName != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_replication") |> filter(fn: (r) => r._field == "lag_ms_num") |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> filter(fn: (r) => r.replica_set_name =~ /^%s$/) |> aggregateWindow(every: 1m, fn: mean, createEmpty: false) |> sort(columns: ["_time"], desc: false)`, timeRange, regexp.QuoteMeta(agentID), regexp.QuoteMeta(replicaSetName))
		} else if agentID != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_replication") |> filter(fn: (r) => r._field == "lag_ms_num") |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> aggregateWindow(every: 1m, fn: mean, createEmpty: false) |> sort(columns: ["_time"], desc: false)`, timeRange, regexp.QuoteMeta(agentID))
		} else {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_replication") |> filter(fn: (r) => r._field == "lag_ms_num") |> aggregateWindow(every: 1m, fn: mean, createEmpty: false) |> sort(columns: ["_time"], desc: false)`, timeRange)
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		influxWriter := server.GetInfluxWriter()
		if influxWriter == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "error",
				"error":  "InfluxDB servisi kullanılamıyor",
			})
			return
		}

		results, err := influxWriter.QueryMetrics(ctx, query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "MongoDB replication lag metrikleri alınamadı: " + err.Error(),
			})
			return
		}

		// Replication lag istatistikleri hesapla
		var totalLag, minLag, maxLag, avgLag float64
		var lagCount int
		var latestTime time.Time
		var latestLag float64
		var latestTimestamp string

		if len(results) > 0 {
			minLag = 99999999 // Büyük bir başlangıç değeri
			maxLag = 0

			for _, result := range results {
				if val, ok := result["_value"]; ok {
					if lagMs, ok := val.(float64); ok && lagMs >= 0 {
						// Milisaniyeden saniyeye çevir
						lagSeconds := lagMs / 1000.0

						totalLag += lagSeconds
						lagCount++

						if lagSeconds < minLag {
							minLag = lagSeconds
						}
						if lagSeconds > maxLag {
							maxLag = lagSeconds
						}

						// En son zaman damgasını bul
						if timestamp, ok := result["_time"]; ok {
							if timeStr, ok := timestamp.(string); ok {
								if parsedTime, err := time.Parse(time.RFC3339, timeStr); err == nil {
									if parsedTime.After(latestTime) {
										latestTime = parsedTime
										latestLag = lagSeconds
										latestTimestamp = timeStr
									}
								}
							}
						}
					}
				}
			}

			if lagCount > 0 {
				avgLag = totalLag / float64(lagCount)
			}

			// Eğer hiç veri yoksa minimum değeri sıfırlayalım
			if lagCount == 0 {
				minLag = 0
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data": gin.H{
				"agent_id": agentID,
				"summary": gin.H{
					"latest_replication_lag_seconds": latestLag,
					"latest_timestamp":               latestTimestamp,
					"avg_replication_lag_seconds":    avgLag,
					"min_replication_lag_seconds":    minLag,
					"max_replication_lag_seconds":    maxLag,
					"total_measurements":             lagCount,
				},
				"all_data": results,
			},
		})
	}
}

// getMongoDBOplogMetrics, MongoDB oplog metriklerini getirir
func getMongoDBOplogMetrics(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := c.Query("agent_id")
		replicaSetName := c.Query("replica_set") // İsteğe bağlı replica set filtresi
		timeRange := c.DefaultQuery("range", "1h")

		// Oplog ile ilgili tüm field'ları dahil et
		oplogFields := "r._field == \"oplog_size_mb\" or r._field == \"oplog_count\" or r._field == \"oplog_max_size_mb\" or r._field == \"oplog_storage_mb\" or r._field == \"oplog_utilization_percent\" or r._field == \"oplog_first_entry_timestamp\" or r._field == \"oplog_last_entry_timestamp\" or r._field == \"oplog_safe_downtime_hours\" or r._field == \"oplog_time_window_hours\" or r._field == \"oplog_time_window_seconds\""

		var query string
		if agentID != "" && replicaSetName != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_replication") |> filter(fn: (r) => %s) |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> filter(fn: (r) => r.replica_set_name =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, oplogFields, regexp.QuoteMeta(agentID), regexp.QuoteMeta(replicaSetName))
		} else if agentID != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_replication") |> filter(fn: (r) => %s) |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, oplogFields, regexp.QuoteMeta(agentID))
		} else {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_replication") |> filter(fn: (r) => %s) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, oplogFields)
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		influxWriter := server.GetInfluxWriter()
		if influxWriter == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "error",
				"error":  "InfluxDB servisi kullanılamıyor",
			})
			return
		}

		results, err := influxWriter.QueryMetrics(ctx, query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "MongoDB oplog metrikleri alınamadı: " + err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   results,
		})
	}
}

// MongoDB Performance Metrics Handlers

// getMongoDBPerformanceQPSMetrics, MongoDB QPS (queries per second) metriklerini getirir
func getMongoDBPerformanceQPSMetrics(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := c.Query("agent_id")
		database := c.Query("database") // İsteğe bağlı database filtresi
		timeRange := c.DefaultQuery("range", "1h")

		var query string
		if agentID != "" && database != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_performance") |> filter(fn: (r) => r._field == "queries_per_sec") |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> filter(fn: (r) => r.database_name =~ /^%s$/) |> aggregateWindow(every: 1m, fn: mean, createEmpty: false)`, timeRange, regexp.QuoteMeta(agentID), regexp.QuoteMeta(database))
		} else if agentID != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_performance") |> filter(fn: (r) => r._field == "queries_per_sec") |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> aggregateWindow(every: 1m, fn: mean, createEmpty: false)`, timeRange, regexp.QuoteMeta(agentID))
		} else {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_performance") |> filter(fn: (r) => r._field == "queries_per_sec") |> aggregateWindow(every: 1m, fn: mean, createEmpty: false)`, timeRange)
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		influxWriter := server.GetInfluxWriter()
		if influxWriter == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "error",
				"error":  "InfluxDB servisi kullanılamıyor",
			})
			return
		}

		results, err := influxWriter.QueryMetrics(ctx, query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "MongoDB QPS metrikleri alınamadı: " + err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   results,
		})
	}
}

// getMongoDBPerformanceReadWriteRatioMetrics, MongoDB Read/Write oranı metriklerini getirir
func getMongoDBPerformanceReadWriteRatioMetrics(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := c.Query("agent_id")
		database := c.Query("database")
		timeRange := c.DefaultQuery("range", "1h")

		var query string
		if agentID != "" && database != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_performance") |> filter(fn: (r) => r._field == "read_write_ratio") |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> filter(fn: (r) => r.database_name =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, regexp.QuoteMeta(agentID), regexp.QuoteMeta(database))
		} else if agentID != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_performance") |> filter(fn: (r) => r._field == "read_write_ratio") |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, regexp.QuoteMeta(agentID))
		} else {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_performance") |> filter(fn: (r) => r._field == "read_write_ratio") |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange)
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		influxWriter := server.GetInfluxWriter()
		if influxWriter == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "error",
				"error":  "InfluxDB servisi kullanılamıyor",
			})
			return
		}

		results, err := influxWriter.QueryMetrics(ctx, query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "MongoDB Read/Write oranı metrikleri alınamadı: " + err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   results,
		})
	}
}

// getMongoDBPerformanceSlowQueriesMetrics, MongoDB yavaş query metriklerini getirir
func getMongoDBPerformanceSlowQueriesMetrics(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := c.Query("agent_id")
		database := c.Query("database")
		timeRange := c.DefaultQuery("range", "1h")

		var query string
		if agentID != "" && database != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_performance") |> filter(fn: (r) => r._field == "slow_queries_count") |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> filter(fn: (r) => r.database_name =~ /^%s$/) |> aggregateWindow(every: 5m, fn: sum, createEmpty: false)`, timeRange, regexp.QuoteMeta(agentID), regexp.QuoteMeta(database))
		} else if agentID != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_performance") |> filter(fn: (r) => r._field == "slow_queries_count") |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> aggregateWindow(every: 5m, fn: sum, createEmpty: false)`, timeRange, regexp.QuoteMeta(agentID))
		} else {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_performance") |> filter(fn: (r) => r._field == "slow_queries_count") |> aggregateWindow(every: 5m, fn: sum, createEmpty: false)`, timeRange)
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		influxWriter := server.GetInfluxWriter()
		if influxWriter == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "error",
				"error":  "InfluxDB servisi kullanılamıyor",
			})
			return
		}

		results, err := influxWriter.QueryMetrics(ctx, query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "MongoDB yavaş query metrikleri alınamadı: " + err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   results,
		})
	}
}

// getMongoDBPerformanceQueryTimeMetrics, MongoDB query süresi metriklerini getirir (avg, p95, p99)
func getMongoDBPerformanceQueryTimeMetrics(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := c.Query("agent_id")
		database := c.Query("database")
		timeRange := c.DefaultQuery("range", "1h")

		var query string
		queryTimeFields := "r._field == \"avg_query_time_ms\" or r._field == \"query_time_p95_ms\" or r._field == \"query_time_p99_ms\""

		if agentID != "" && database != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_performance") |> filter(fn: (r) => %s) |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> filter(fn: (r) => r.database_name =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, queryTimeFields, regexp.QuoteMeta(agentID), regexp.QuoteMeta(database))
		} else if agentID != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_performance") |> filter(fn: (r) => %s) |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, queryTimeFields, regexp.QuoteMeta(agentID))
		} else {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_performance") |> filter(fn: (r) => %s) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, queryTimeFields)
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		influxWriter := server.GetInfluxWriter()
		if influxWriter == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "error",
				"error":  "InfluxDB servisi kullanılamıyor",
			})
			return
		}

		results, err := influxWriter.QueryMetrics(ctx, query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "MongoDB query süresi metrikleri alınamadı: " + err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   results,
		})
	}
}

// getMongoDBPerformanceActiveQueriesMetrics, MongoDB aktif query metriklerini getirir
func getMongoDBPerformanceActiveQueriesMetrics(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := c.Query("agent_id")
		database := c.Query("database")
		timeRange := c.DefaultQuery("range", "1h")

		var query string
		if agentID != "" && database != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_performance") |> filter(fn: (r) => r._field == "active_queries_count") |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> filter(fn: (r) => r.database_name =~ /^%s$/) |> aggregateWindow(every: 1m, fn: mean, createEmpty: false)`, timeRange, regexp.QuoteMeta(agentID), regexp.QuoteMeta(database))
		} else if agentID != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_performance") |> filter(fn: (r) => r._field == "active_queries_count") |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> aggregateWindow(every: 1m, fn: mean, createEmpty: false)`, timeRange, regexp.QuoteMeta(agentID))
		} else {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_performance") |> filter(fn: (r) => r._field == "active_queries_count") |> aggregateWindow(every: 1m, fn: mean, createEmpty: false)`, timeRange)
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		influxWriter := server.GetInfluxWriter()
		if influxWriter == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "error",
				"error":  "InfluxDB servisi kullanılamıyor",
			})
			return
		}

		results, err := influxWriter.QueryMetrics(ctx, query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "MongoDB aktif query metrikleri alınamadı: " + err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   results,
		})
	}
}

// getMongoDBActiveQueriesDetails, MongoDB aktif query detaylarını getirir
func getMongoDBActiveQueriesDetails(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := c.Query("agent_id")
		database := c.Query("database")
		timeRange := c.DefaultQuery("range", "10m") // Varsayılan olarak son 10 dakika
		limit := c.DefaultQuery("limit", "50")      // Varsayılan olarak en fazla 50 sorgu

		var query string
		if agentID != "" && database != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_active_operation") |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> filter(fn: (r) => r.database == "%s") |> pivot(rowKey:["_time"], columnKey: ["_field"], valueColumn: "_value") |> sort(columns: ["_time"], desc: true) |> limit(n: %s)`, timeRange, regexp.QuoteMeta(agentID), database, limit)
		} else if agentID != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_active_operation") |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> pivot(rowKey:["_time"], columnKey: ["_field"], valueColumn: "_value") |> sort(columns: ["_time"], desc: true) |> limit(n: %s)`, timeRange, regexp.QuoteMeta(agentID), limit)
		} else {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_active_operation") |> pivot(rowKey:["_time"], columnKey: ["_field"], valueColumn: "_value") |> sort(columns: ["_time"], desc: true) |> limit(n: %s)`, timeRange, limit)
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		influxWriter := server.GetInfluxWriter()
		if influxWriter == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "error",
				"error":  "InfluxDB servisi kullanılamıyor",
			})
			return
		}

		results, err := influxWriter.QueryMetrics(ctx, query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "MongoDB aktif query detayları alınamadı: " + err.Error(),
			})
			return
		}

		// MongoDB Active Operation struct
		type MongoActiveOperation struct {
			OperationID    string          `json:"operation_id,omitempty"`
			OpType         string          `json:"op_type"`
			Namespace      string          `json:"namespace"`
			Command        string          `json:"command,omitempty"`
			CommandDetails json.RawMessage `json:"command_details,omitempty"`

			// 🆕 Truncated command handling için yeni alanlar
			CommandTruncated    bool            `json:"command_truncated,omitempty"`
			CommandType         string          `json:"command_type,omitempty"`
			TruncatedContent    json.RawMessage `json:"truncated_content,omitempty"`
			InferredCommandType string          `json:"inferred_command_type,omitempty"`
			PlanSummary         string          `json:"plan_summary,omitempty"`
			Note                string          `json:"note,omitempty"`

			DurationSeconds float64 `json:"duration_seconds"`
			Database        string  `json:"database"`
			Collection      string  `json:"collection,omitempty"`
			Client          string  `json:"client,omitempty"`

			// 🆕 Detaylı client bilgileri
			ClientDetails json.RawMessage `json:"client_details,omitempty"`

			ConnectionID   int64     `json:"connection_id,omitempty"`
			ThreadID       string    `json:"thread_id,omitempty"`
			WaitingForLock bool      `json:"waiting_for_lock"` // ← omitempty kaldırıldı, her zaman gösterilsin
			LockType       string    `json:"lock_type,omitempty"`
			Timestamp      time.Time `json:"timestamp"`
		}

		activeOperations := make([]MongoActiveOperation, 0)
		for _, result := range results {
			var operation MongoActiveOperation

			// Timestamp
			if timeValue, ok := result["_time"].(time.Time); ok {
				operation.Timestamp = timeValue
			}

			// Fields
			if opType, ok := result["op_type"].(string); ok {
				operation.OpType = opType
			}
			if namespace, ok := result["namespace"].(string); ok {
				operation.Namespace = namespace
			}
			if command, ok := result["command"].(string); ok {
				operation.Command = command
			}
			if commandDetails, ok := result["command_details"].(string); ok {
				operation.CommandDetails = json.RawMessage(commandDetails)
			}

			// 🆕 Yeni truncated command alanları
			if commandTruncated, ok := result["command_truncated"].(bool); ok {
				operation.CommandTruncated = commandTruncated
			}
			if commandType, ok := result["command_type"].(string); ok {
				operation.CommandType = commandType
			}
			if truncatedContent, ok := result["truncated_content"].(string); ok {
				operation.TruncatedContent = json.RawMessage(truncatedContent)
			}
			if inferredCommandType, ok := result["inferred_command_type"].(string); ok {
				operation.InferredCommandType = inferredCommandType
			}
			if planSummary, ok := result["plan_summary"].(string); ok {
				operation.PlanSummary = planSummary
			}
			if note, ok := result["note"].(string); ok {
				operation.Note = note
			}

			if duration, ok := result["duration"].(float64); ok {
				operation.DurationSeconds = duration
			}
			if operationID, ok := result["operation_id"].(string); ok {
				operation.OperationID = operationID
			}

			// Tags
			if db, ok := result["database"].(string); ok {
				operation.Database = db
			}
			if collection, ok := result["collection"].(string); ok {
				operation.Collection = collection
			}
			if client, ok := result["client"].(string); ok {
				operation.Client = client
			}
			if clientDetails, ok := result["client_details"].(string); ok {
				operation.ClientDetails = json.RawMessage(clientDetails)
			}
			if connID, ok := result["connection_id"].(float64); ok {
				operation.ConnectionID = int64(connID)
			} else if connIDInt, ok := result["connection_id"].(int64); ok {
				operation.ConnectionID = connIDInt
			}
			if threadID, ok := result["thread_id"].(string); ok {
				operation.ThreadID = threadID
			}
			if waitingForLock, ok := result["waiting_for_lock"].(bool); ok {
				operation.WaitingForLock = waitingForLock
			}
			if lockType, ok := result["lock_type"].(string); ok {
				operation.LockType = lockType
			}

			activeOperations = append(activeOperations, operation)
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   activeOperations,
		})
	}
}

// getMongoDBPerformanceProfilerMetrics, MongoDB profiler metriklerini getirir
func getMongoDBPerformanceProfilerMetrics(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := c.Query("agent_id")
		timeRange := c.DefaultQuery("range", "1h")

		var query string
		if agentID != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_performance") |> filter(fn: (r) => r._field == "profiler_enabled_dbs") |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, regexp.QuoteMeta(agentID))
		} else {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_performance") |> filter(fn: (r) => r._field == "profiler_enabled_dbs") |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange)
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		influxWriter := server.GetInfluxWriter()
		if influxWriter == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "error",
				"error":  "InfluxDB servisi kullanılamıyor",
			})
			return
		}

		results, err := influxWriter.QueryMetrics(ctx, query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "MongoDB profiler metrikleri alınamadı: " + err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   results,
		})
	}
}

// getMongoDBPerformanceAllMetrics, tüm MongoDB performance metriklerini tek seferde getirir
func getMongoDBPerformanceAllMetrics(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := c.Query("agent_id")
		database := c.Query("database")
		timeRange := c.DefaultQuery("range", "1h")

		// Sadece numerik field'ları dahil et (string field'lar mean() ile agregatlanamaz)
		numericFields := "r._field == \"queries_per_sec\" or r._field == \"read_write_ratio\" or r._field == \"slow_queries_count\" or r._field == \"avg_query_time_ms\" or r._field == \"query_time_p95_ms\" or r._field == \"query_time_p99_ms\" or r._field == \"active_queries_count\" or r._field == \"profiler_enabled_dbs\""

		var query string
		if agentID != "" && database != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_performance") |> filter(fn: (r) => %s) |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> filter(fn: (r) => r.database_name =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, numericFields, regexp.QuoteMeta(agentID), regexp.QuoteMeta(database))
		} else if agentID != "" {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_performance") |> filter(fn: (r) => %s) |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, numericFields, regexp.QuoteMeta(agentID))
		} else {
			query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_performance") |> filter(fn: (r) => %s) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, numericFields)
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		influxWriter := server.GetInfluxWriter()
		if influxWriter == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "error",
				"error":  "InfluxDB servisi kullanılamıyor",
			})
			return
		}

		results, err := influxWriter.QueryMetrics(ctx, query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "MongoDB performance metrikleri alınamadı: " + err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   results,
		})
	}
}

// MongoDB Dashboard Endpoint - Tüm MongoDB metriklerini tek seferde getirir

// getMongoDBDashboardMetrics, MongoDB dashboard için tüm metrikleri tek seferde getirir
func getMongoDBDashboardMetrics(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := c.Query("agent_id")
		database := c.Query("database")
		replicaSetName := c.Query("replica_set")
		timeRange := c.DefaultQuery("range", "1h")

		ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second) // Dashboard için daha uzun timeout
		defer cancel()

		influxWriter := server.GetInfluxWriter()
		if influxWriter == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "error",
				"error":  "InfluxDB servisi kullanılamıyor",
			})
			return
		}

		// Paralel sorgu kanalları
		type QueryResult struct {
			Category string
			Data     []map[string]interface{}
			Error    error
		}

		resultChan := make(chan QueryResult, 10) // 10 kategori için kanal

		// 1. System Metrics Query
		go func() {
			systemFields := "r._field == \"cpu_usage\" or r._field == \"cpu_cores\" or r._field == \"memory_usage\" or r._field == \"total_memory\" or r._field == \"free_memory\" or r._field == \"total_disk\" or r._field == \"free_disk\" or r._field == \"filesystem\" or r._field == \"mount_point\" or r._field == \"usage_percent\" or r._field == \"response_time_ms\""
			var query string
			if agentID != "" {
				query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_system") |> filter(fn: (r) => %s) |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, systemFields, regexp.QuoteMeta(agentID))
			} else {
				query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_system") |> filter(fn: (r) => %s) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, systemFields)
			}
			results, err := influxWriter.QueryMetrics(ctx, query)
			resultChan <- QueryResult{Category: "system_metrics", Data: results, Error: err}
		}()

		// 2. Connection Metrics Query
		go func() {
			connectionFields := "r._field == \"current\" or r._field == \"available\" or r._field == \"total_created_connections\""
			var query string
			if agentID != "" {
				query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_connections") |> filter(fn: (r) => %s) |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, connectionFields, regexp.QuoteMeta(agentID))
			} else {
				query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_connections") |> filter(fn: (r) => %s) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, connectionFields)
			}
			results, err := influxWriter.QueryMetrics(ctx, query)
			resultChan <- QueryResult{Category: "connection_metrics", Data: results, Error: err}
		}()

		// 3. Operations Metrics Query
		go func() {
			operationFields := "r._field == \"insert\" or r._field == \"query\" or r._field == \"update\" or r._field == \"delete\" or r._field == \"getmore\" or r._field == \"command\""
			var query string
			if agentID != "" && database != "" {
				query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_operations") |> filter(fn: (r) => %s) |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> filter(fn: (r) => r.database_name =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, operationFields, regexp.QuoteMeta(agentID), regexp.QuoteMeta(database))
			} else if agentID != "" {
				query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_operations") |> filter(fn: (r) => %s) |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, operationFields, regexp.QuoteMeta(agentID))
			} else {
				query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_operations") |> filter(fn: (r) => %s) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, operationFields)
			}
			results, err := influxWriter.QueryMetrics(ctx, query)
			resultChan <- QueryResult{Category: "operation_metrics", Data: results, Error: err}
		}()

		// 4. Storage Metrics Query
		go func() {
			storageFields := "r._field == \"data_size_mb\" or r._field == \"storage_size_mb\" or r._field == \"index_size_mb\" or r._field == \"avg_obj_size\" or r._field == \"file_size_mb\""
			var query string
			if agentID != "" && database != "" {
				query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_storage") |> filter(fn: (r) => %s) |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> filter(fn: (r) => r.database_name =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, storageFields, regexp.QuoteMeta(agentID), regexp.QuoteMeta(database))
			} else if agentID != "" {
				query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_storage") |> filter(fn: (r) => %s) |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, storageFields, regexp.QuoteMeta(agentID))
			} else {
				query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_storage") |> filter(fn: (r) => %s) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, storageFields)
			}
			results, err := influxWriter.QueryMetrics(ctx, query)
			resultChan <- QueryResult{Category: "storage_metrics", Data: results, Error: err}
		}()

		// 5. Database Info Metrics Query
		go func() {
			databaseFields := "r._field == \"database_count\" or r._field == \"collection_count\""
			var query string
			if agentID != "" && database != "" {
				query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_database") |> filter(fn: (r) => %s) |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> filter(fn: (r) => r.database_name =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, databaseFields, regexp.QuoteMeta(agentID), regexp.QuoteMeta(database))
			} else if agentID != "" {
				query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_database") |> filter(fn: (r) => %s) |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, databaseFields, regexp.QuoteMeta(agentID))
			} else {
				query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_database") |> filter(fn: (r) => %s) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, databaseFields)
			}
			results, err := influxWriter.QueryMetrics(ctx, query)
			resultChan <- QueryResult{Category: "database_info_metrics", Data: results, Error: err}
		}()

		// 6. Replication Status Metrics Query
		go func() {
			replicationFields := "r._field == \"member_state\" or r._field == \"members_healthy\" or r._field == \"members_total\""
			var query string
			if agentID != "" && replicaSetName != "" {
				query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_replication") |> filter(fn: (r) => %s) |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> filter(fn: (r) => r.replica_set_name =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, replicationFields, regexp.QuoteMeta(agentID), regexp.QuoteMeta(replicaSetName))
			} else if agentID != "" {
				query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_replication") |> filter(fn: (r) => %s) |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, replicationFields, regexp.QuoteMeta(agentID))
			} else {
				query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_replication") |> filter(fn: (r) => %s) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, replicationFields)
			}
			results, err := influxWriter.QueryMetrics(ctx, query)
			resultChan <- QueryResult{Category: "replication_status_metrics", Data: results, Error: err}
		}()

		// 7. Replication Lag Metrics Query
		go func() {
			var query string
			if agentID != "" && replicaSetName != "" {
				query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_replication") |> filter(fn: (r) => r._field == "lag_ms_num") |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> filter(fn: (r) => r.replica_set_name =~ /^%s$/) |> aggregateWindow(every: 1m, fn: mean, createEmpty: false)`, timeRange, regexp.QuoteMeta(agentID), regexp.QuoteMeta(replicaSetName))
			} else if agentID != "" {
				query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_replication") |> filter(fn: (r) => r._field == "lag_ms_num") |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> aggregateWindow(every: 1m, fn: mean, createEmpty: false)`, timeRange, regexp.QuoteMeta(agentID))
			} else {
				query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_replication") |> filter(fn: (r) => r._field == "lag_ms_num") |> aggregateWindow(every: 1m, fn: mean, createEmpty: false)`, timeRange)
			}
			results, err := influxWriter.QueryMetrics(ctx, query)
			resultChan <- QueryResult{Category: "replication_lag_metrics", Data: results, Error: err}
		}()

		// 8. Oplog Metrics Query (numerik alanlar için mean, timestamp alanlar için last)
		go func() {
			// Numerik oplog field'ları için mean agregasyonu
			oplogNumericFields := "r._field == \"oplog_size_mb\" or r._field == \"oplog_count\" or r._field == \"oplog_max_size_mb\" or r._field == \"oplog_storage_mb\" or r._field == \"oplog_utilization_percent\" or r._field == \"oplog_safe_downtime_hours\" or r._field == \"oplog_time_window_hours\" or r._field == \"oplog_time_window_seconds\""
			var query string
			if agentID != "" && replicaSetName != "" {
				query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_replication") |> filter(fn: (r) => %s) |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> filter(fn: (r) => r.replica_set_name =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, oplogNumericFields, regexp.QuoteMeta(agentID), regexp.QuoteMeta(replicaSetName))
			} else if agentID != "" {
				query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_replication") |> filter(fn: (r) => %s) |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, oplogNumericFields, regexp.QuoteMeta(agentID))
			} else {
				query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_replication") |> filter(fn: (r) => %s) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, oplogNumericFields)
			}
			results, err := influxWriter.QueryMetrics(ctx, query)
			resultChan <- QueryResult{Category: "oplog_metrics", Data: results, Error: err}
		}()

		// 9. Performance Metrics Query
		go func() {
			performanceFields := "r._field == \"queries_per_sec\" or r._field == \"read_write_ratio\" or r._field == \"slow_queries_count\" or r._field == \"avg_query_time_ms\" or r._field == \"query_time_p95_ms\" or r._field == \"query_time_p99_ms\" or r._field == \"active_queries_count\" or r._field == \"profiler_enabled_dbs\""
			var query string
			if agentID != "" && database != "" {
				query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_performance") |> filter(fn: (r) => %s) |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> filter(fn: (r) => r.database_name =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, performanceFields, regexp.QuoteMeta(agentID), regexp.QuoteMeta(database))
			} else if agentID != "" {
				query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_performance") |> filter(fn: (r) => %s) |> filter(fn: (r) => r.agent_id =~ /^%s$/) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, performanceFields, regexp.QuoteMeta(agentID))
			} else {
				query = fmt.Sprintf(`from(bucket: "clustereye") |> range(start: -%s) |> filter(fn: (r) => r._measurement == "mongodb_performance") |> filter(fn: (r) => %s) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false)`, timeRange, performanceFields)
			}
			results, err := influxWriter.QueryMetrics(ctx, query)
			resultChan <- QueryResult{Category: "performance_metrics", Data: results, Error: err}
		}()

		// Sonuçları topla
		dashboardData := gin.H{
			"agent_id":     agentID,
			"database":     database,
			"replica_set":  replicaSetName,
			"time_range":   timeRange,
			"generated_at": time.Now().Format(time.RFC3339),
		}

		var errors []string

		// 9 kategoriyi bekle
		for i := 0; i < 9; i++ {
			select {
			case result := <-resultChan:
				if result.Error != nil {
					errors = append(errors, fmt.Sprintf("%s: %s", result.Category, result.Error.Error()))
					dashboardData[result.Category] = gin.H{
						"status": "error",
						"error":  result.Error.Error(),
						"data":   []interface{}{},
					}
				} else {
					dashboardData[result.Category] = gin.H{
						"status":     "success",
						"data_count": len(result.Data),
						"data":       result.Data,
					}
				}
			case <-ctx.Done():
				errors = append(errors, "Context timeout")
			}
		}

		// Toplam özet bilgileri
		dashboardData["summary"] = gin.H{
			"total_categories": 9,
			"error_count":      len(errors),
			"success_count":    9 - len(errors),
		}

		if len(errors) > 0 {
			dashboardData["errors"] = errors
		}

		// Response durumunu belirle
		var statusCode int
		var status string
		if len(errors) == 9 {
			statusCode = http.StatusInternalServerError
			status = "error"
		} else if len(errors) > 0 {
			statusCode = http.StatusPartialContent
			status = "partial_success"
		} else {
			statusCode = http.StatusOK
			status = "success"
		}

		dashboardData["status"] = status

		c.JSON(statusCode, dashboardData)
	}
}

// getMongoDBDatabaseListMetrics returns the list of available databases for an agent
func getMongoDBDatabaseListMetrics(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := c.Query("agent_id")
		if agentID == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "agent_id parameter is required",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		// Agent'dan MongoDB database listesini al
		databases, err := server.SendMongoDatabaseListQuery(ctx, agentID)
		if err != nil {
			// Hata tipine göre farklı status kodları döndür
			if strings.Contains(err.Error(), "agent bulunamadı") || strings.Contains(err.Error(), "bağlantı kapalı") {
				c.JSON(http.StatusNotFound, gin.H{
					"error": fmt.Sprintf("Agent not found or disconnected: %s", agentID),
				})
			} else if strings.Contains(err.Error(), "context deadline exceeded") {
				c.JSON(http.StatusRequestTimeout, gin.H{
					"error": "Request timeout while waiting for agent response",
				})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": fmt.Sprintf("Failed to get database list from agent: %v", err),
				})
			}
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   databases,
		})
	}
}

// getMongoDBCollectionMetrics returns historical collection-level metrics for a specific database
func getMongoDBCollectionMetrics(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := c.Query("agent_id")
		database := c.Query("database")
		
		if agentID == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "agent_id parameter is required",
			})
			return
		}
		
		if database == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "database parameter is required",
			})
			return
		}

		timeRange := c.DefaultQuery("range", "1h")


		// Working query without aggregation - we'll do processing in Go
		query := fmt.Sprintf(`
			from(bucket: "clustereye")
			|> range(start: -%s)
			|> filter(fn: (r) => r._measurement == "mongodb_collection")
			|> filter(fn: (r) => r.agent_id == "%s")
			|> filter(fn: (r) => r.database == "%s")
			|> filter(fn: (r) => r._field == "data_size_mb" or r._field == "document_count" or r._field == "index_count" or r._field == "index_size_mb" or r._field == "storage_size_mb")
		`, timeRange, agentID, database)

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		influxWriter := server.GetInfluxWriter()
		if influxWriter == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "InfluxDB servisi kullanılamıyor",
			})
			return
		}

		results, err := influxWriter.QueryMetrics(ctx, query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("Failed to query collection metrics: %v", err),
			})
			return
		}

		// Manual aggregation - group by time windows and collection
		aggregateWindowSeconds := int64(60)  // 1 minute default
		if timeRange == "1h" || timeRange == "3h" {
			aggregateWindowSeconds = 60   // 1 minute
		} else if timeRange == "1d" {
			aggregateWindowSeconds = 300  // 5 minutes
		} else if timeRange == "7d" {
			aggregateWindowSeconds = 3600 // 1 hour
		}

		timeSeriesData := make(map[string]map[int64]map[string]interface{}) // collection -> timestamp -> fields

		for _, result := range results {
			collName, hasCollection := result["collection"]
			if !hasCollection || collName == nil {
				continue
			}
			collNameStr := fmt.Sprintf("%v", collName)

			field, hasField := result["_field"]
			if !hasField || field == nil {
				continue
			}
			fieldStr := fmt.Sprintf("%v", field)

			value := result["_value"]
			timestamp := result["_time"]

			if timestamp == nil || value == nil {
				continue
			}

			// Parse timestamp and round to aggregate window
			var timeUnix int64
			switch t := timestamp.(type) {
			case string:
				if parsedTime, err := time.Parse(time.RFC3339, t); err == nil {
					timeUnix = parsedTime.Unix()
				} else {
					continue
				}
			case time.Time:
				timeUnix = t.Unix()
			default:
				continue
			}
			
			// Round to aggregate window
			roundedTime := (timeUnix / aggregateWindowSeconds) * aggregateWindowSeconds

			// Initialize nested maps if not exists
			if _, exists := timeSeriesData[collNameStr]; !exists {
				timeSeriesData[collNameStr] = make(map[int64]map[string]interface{})
			}
			if _, exists := timeSeriesData[collNameStr][roundedTime]; !exists {
				timeSeriesData[collNameStr][roundedTime] = map[string]interface{}{
					"timestamp":  time.Unix(roundedTime, 0).Format(time.RFC3339),
					"collection": collNameStr,
					"database":   database,
				}
			}

			// Add the field value (take latest value in window)
			timeSeriesData[collNameStr][roundedTime][fieldStr] = value
		}

		// Convert to response format for historical data
		var collectionsHistorical []map[string]interface{}

		for collName, timeWindowMap := range timeSeriesData {
			for _, timePoint := range timeWindowMap {
				historicalPoint := map[string]interface{}{
					"collection": collName,
					"database":   database,
					"timestamp":  timePoint["timestamp"],
				}

				// Add metrics with proper type conversion - using actual InfluxDB field names
				if val, exists := timePoint["document_count"]; exists && val != nil {
					if floatVal, ok := val.(float64); ok {
						historicalPoint["document_count"] = int64(floatVal)
					} else if intVal, ok := val.(int64); ok {
						historicalPoint["document_count"] = intVal
					} else {
						historicalPoint["document_count"] = 0
					}
				} else {
					historicalPoint["document_count"] = 0
				}

				if val, exists := timePoint["data_size_mb"]; exists && val != nil {
					if floatVal, ok := val.(float64); ok {
						historicalPoint["data_size_mb"] = floatVal
					} else if intVal, ok := val.(int64); ok {
						historicalPoint["data_size_mb"] = float64(intVal)
					} else {
						historicalPoint["data_size_mb"] = 0.0
					}
				} else {
					historicalPoint["data_size_mb"] = 0.0
				}

				if val, exists := timePoint["index_count"]; exists && val != nil {
					if floatVal, ok := val.(float64); ok {
						historicalPoint["index_count"] = int64(floatVal)
					} else if intVal, ok := val.(int64); ok {
						historicalPoint["index_count"] = intVal
					} else {
						historicalPoint["index_count"] = 0
					}
				} else {
					historicalPoint["index_count"] = 0
				}

				if val, exists := timePoint["index_size_mb"]; exists && val != nil {
					if floatVal, ok := val.(float64); ok {
						historicalPoint["index_size_mb"] = floatVal
					} else if intVal, ok := val.(int64); ok {
						historicalPoint["index_size_mb"] = float64(intVal)
					} else {
						historicalPoint["index_size_mb"] = 0.0
					}
				} else {
					historicalPoint["index_size_mb"] = 0.0
				}

				if val, exists := timePoint["storage_size_mb"]; exists && val != nil {
					if floatVal, ok := val.(float64); ok {
						historicalPoint["storage_size_mb"] = floatVal
					} else if intVal, ok := val.(int64); ok {
						historicalPoint["storage_size_mb"] = float64(intVal)
					} else {
						historicalPoint["storage_size_mb"] = 0.0
					}
				} else {
					historicalPoint["storage_size_mb"] = 0.0
				}

				collectionsHistorical = append(collectionsHistorical, historicalPoint)
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   collectionsHistorical,
		})
	}
}

// getMongoDBCollectionMetricsCurrent returns current (latest) collection-level metrics for table display
func getMongoDBCollectionMetricsCurrent(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := c.Query("agent_id")
		database := c.Query("database")
		
		if agentID == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "agent_id parameter is required",
			})
			return
		}
		
		if database == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "database parameter is required",
			})
			return
		}

		// Query to get current (latest) collection metrics from InfluxDB
		query := fmt.Sprintf(`
			from(bucket: "clustereye")
			|> range(start: -24h)
			|> filter(fn: (r) => r._measurement == "mongodb_collection")
			|> filter(fn: (r) => r.agent_id == "%s")
			|> filter(fn: (r) => r.database == "%s")
			|> group(columns: ["collection", "_field"])
			|> last()
		`, agentID, database)

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		influxWriter := server.GetInfluxWriter()
		if influxWriter == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "InfluxDB servisi kullanılamıyor",
			})
			return
		}

		results, err := influxWriter.QueryMetrics(ctx, query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("Failed to query current collection metrics: %v", err),
			})
			return
		}

		// Group results by collection for current data
		collectionData := make(map[string]map[string]interface{})

		for _, result := range results {
			collName, hasCollection := result["collection"]
			if !hasCollection || collName == nil {
				continue
			}
			collNameStr := fmt.Sprintf("%v", collName)

			field, hasField := result["_field"]
			if !hasField || field == nil {
				continue
			}
			fieldStr := fmt.Sprintf("%v", field)

			value := result["_value"]

			// Initialize collection data if not exists
			if _, exists := collectionData[collNameStr]; !exists {
				collectionData[collNameStr] = map[string]interface{}{
					"collection": collNameStr,
					"database":   database,
				}
			}

			// Add the field value
			if value != nil {
				collectionData[collNameStr][fieldStr] = value
			}
		}

		// Convert to slice with proper type conversion (current snapshot)
		var collections []map[string]interface{}
		for _, collData := range collectionData {
			collection := map[string]interface{}{
				"collection": collData["collection"],
				"database":   collData["database"],
			}

			// Add metrics with proper type conversion and defaults
			if val, exists := collData["document_count"]; exists && val != nil {
				if floatVal, ok := val.(float64); ok {
					collection["document_count"] = int64(floatVal)
				} else if intVal, ok := val.(int64); ok {
					collection["document_count"] = intVal
				} else {
					collection["document_count"] = 0
				}
			} else {
				collection["document_count"] = 0
			}

			if val, exists := collData["data_size_mb"]; exists && val != nil {
				if floatVal, ok := val.(float64); ok {
					collection["data_size_mb"] = floatVal
				} else if intVal, ok := val.(int64); ok {
					collection["data_size_mb"] = float64(intVal)
				} else {
					collection["data_size_mb"] = 0.0
				}
			} else {
				collection["data_size_mb"] = 0.0
			}

			if val, exists := collData["storage_size_mb"]; exists && val != nil {
				if floatVal, ok := val.(float64); ok {
					collection["storage_size_mb"] = floatVal
				} else if intVal, ok := val.(int64); ok {
					collection["storage_size_mb"] = float64(intVal)
				} else {
					collection["storage_size_mb"] = 0.0
				}
			} else {
				collection["storage_size_mb"] = 0.0
			}

			if val, exists := collData["index_size_mb"]; exists && val != nil {
				if floatVal, ok := val.(float64); ok {
					collection["index_size_mb"] = floatVal
				} else if intVal, ok := val.(int64); ok {
					collection["index_size_mb"] = float64(intVal)
				} else {
					collection["index_size_mb"] = 0.0
				}
			} else {
				collection["index_size_mb"] = 0.0
			}

			if val, exists := collData["avg_document_size_bytes"]; exists && val != nil {
				if floatVal, ok := val.(float64); ok {
					collection["avg_document_size_bytes"] = int64(floatVal)
				} else if intVal, ok := val.(int64); ok {
					collection["avg_document_size_bytes"] = intVal
				} else {
					collection["avg_document_size_bytes"] = 0
				}
			} else {
				collection["avg_document_size_bytes"] = 0
			}

			if val, exists := collData["index_count"]; exists && val != nil {
				if floatVal, ok := val.(float64); ok {
					collection["index_count"] = int64(floatVal)
				} else if intVal, ok := val.(int64); ok {
					collection["index_count"] = intVal
				} else {
					collection["index_count"] = 0
				}
			} else {
				collection["index_count"] = 0
			}

			collections = append(collections, collection)
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   collections,
		})
	}
}

// getMongoDBIndexStats, belirtilen veritabanı için index kullanım istatistiklerini getirir
func getMongoDBIndexStats(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		dbName := c.Param("dbName")
		agentID := c.Query("agent_id")

		if dbName == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"status": "error",
				"error":  "Database name is required",
			})
			return
		}

		if agentID == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"status": "error",
				"error":  "Agent ID is required",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
		defer cancel()

		// Agent'tan index stats komutunu çalıştır
		result, err := server.ExecuteMongoCommand(ctx, agentID, "getIndexStats", map[string]interface{}{
			"database": dbName,
		})

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "Index stats alınamadı: " + err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   result,
		})
	}
}
