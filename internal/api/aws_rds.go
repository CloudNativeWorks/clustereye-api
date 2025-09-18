package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	_ "github.com/denisenkom/go-mssqldb" // SQL Server driver
	"github.com/gin-gonic/gin"
	"github.com/CloudNativeWorks/clustereye-api/internal/logger"
	"github.com/CloudNativeWorks/clustereye-api/internal/server"
)

// AWSCredentials, AWS kimlik bilgilerini temsil eder
type AWSCredentials struct {
	AccessKeyID     string `json:"accessKeyId" binding:"required"`
	SecretAccessKey string `json:"secretAccessKey" binding:"required"`
	Region          string `json:"region" binding:"required"`
	SessionToken    string `json:"sessionToken,omitempty"`
}

// TestCredentialsResponse, AWS kimlik bilgileri test sonucunu temsil eder
type TestCredentialsResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// RDSInstancesRequest, RDS instance'larını getirme isteğini temsil eder
type RDSInstancesRequest struct {
	Region      string         `json:"region" binding:"required"`
	Credentials AWSCredentials `json:"credentials" binding:"required"`
}

// RDSInstancesResponse, RDS instance'ları getirme yanıtını temsil eder
type RDSInstancesResponse struct {
	Status    string                `json:"status"`
	Instances []rdstypes.DBInstance `json:"instances,omitempty"`
	Error     string                `json:"error,omitempty"`
}

// CloudWatchMetricsRequest, CloudWatch metriklerini getirme isteğini temsil eder
type CloudWatchMetricsRequest struct {
	InstanceID  string         `json:"instanceId" binding:"required"`
	MetricName  string         `json:"metricName" binding:"required"`
	Region      string         `json:"region" binding:"required"`
	Range       string         `json:"range"`
	Credentials AWSCredentials `json:"credentials" binding:"required"`
}

// MetricDataPoint, bir metrik veri noktasını temsil eder
type MetricDataPoint struct {
	Timestamp string  `json:"timestamp"`
	Value     float64 `json:"value"`
	Unit      string  `json:"unit"`
}

// CloudWatchMetricsResponse, CloudWatch metrikleri getirme yanıtını temsil eder
type CloudWatchMetricsResponse struct {
	Status string            `json:"status"`
	Data   []MetricDataPoint `json:"data,omitempty"`
	Error  string            `json:"error,omitempty"`
}

// SaveRDSInfoRequest, RDS bilgilerini kaydetme isteğini temsil eder
type SaveRDSInfoRequest struct {
	ClusterName  string              `json:"clusterName" binding:"required"`
	Region       string              `json:"region" binding:"required"`
	InstanceInfo rdstypes.DBInstance `json:"instanceInfo" binding:"required"`
	Credentials  AWSCredentials      `json:"credentials" binding:"required"`
	AWSAccountID string              `json:"awsAccountId,omitempty"`
}

// SaveRDSInfoResponse, RDS bilgilerini kaydetme yanıtını temsil eder
type SaveRDSInfoResponse struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// SQLConnectionRequest, SQL Server bağlantı test isteğini temsil eder
type SQLConnectionRequest struct {
	Endpoint string `json:"endpoint" binding:"required"`
	Port     int    `json:"port" binding:"required"`
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Database string `json:"database" binding:"required"`
}

// SQLConnectionResponse, SQL Server bağlantı test yanıtını temsil eder
type SQLConnectionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// GetRDSInstancesResponse, kayıtlı RDS instance'ları getirme yanıtını temsil eder
type GetRDSInstancesResponse struct {
	Success bool                     `json:"success"`
	Data    []map[string]interface{} `json:"data,omitempty"`
	Error   string                   `json:"error,omitempty"`
}

// validateAWSCredentials, AWS kimlik bilgilerini doğrular
func validateAWSCredentials(creds AWSCredentials) error {
	if creds.AccessKeyID == "" {
		return fmt.Errorf("missing required field: accessKeyId")
	}
	if creds.SecretAccessKey == "" {
		return fmt.Errorf("missing required field: secretAccessKey")
	}
	if creds.Region == "" {
		return fmt.Errorf("missing required field: region")
	}

	// Access Key ID formatını kontrol et
	if !strings.HasPrefix(creds.AccessKeyID, "AKIA") && !strings.HasPrefix(creds.AccessKeyID, "ASIA") {
		return fmt.Errorf("invalid Access Key ID format")
	}

	// Secret Access Key uzunluğunu kontrol et
	if len(creds.SecretAccessKey) < 40 {
		return fmt.Errorf("invalid Secret Access Key format")
	}

	return nil
}

// createAWSConfig, AWS konfigürasyonu oluşturur
func createAWSConfig(creds AWSCredentials) (aws.Config, error) {
	return config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(creds.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			creds.AccessKeyID,
			creds.SecretAccessKey,
			creds.SessionToken,
		)),
	)
}

// TestAWSCredentials, AWS kimlik bilgilerini test eder
func TestAWSCredentials(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		var creds AWSCredentials
		if err := c.ShouldBindJSON(&creds); err != nil {
			logger.Error().Err(err).Msg("AWS credentials test - invalid request format")
			c.JSON(http.StatusBadRequest, TestCredentialsResponse{
				Success: false,
				Error:   "Invalid request format: " + err.Error(),
			})
			return
		}

		// Kimlik bilgilerini doğrula
		if err := validateAWSCredentials(creds); err != nil {
			logger.Error().Err(err).Msg("AWS credentials test - validation failed")
			c.JSON(http.StatusBadRequest, TestCredentialsResponse{
				Success: false,
				Error:   err.Error(),
			})
			return
		}

		// AWS konfigürasyonu oluştur
		cfg, err := createAWSConfig(creds)
		if err != nil {
			logger.Error().Err(err).Msg("AWS credentials test - failed to create config")
			c.JSON(http.StatusInternalServerError, TestCredentialsResponse{
				Success: false,
				Error:   "Failed to create AWS config: " + err.Error(),
			})
			return
		}

		// STS GetCallerIdentity ile kimlik bilgilerini test et
		stsClient := sts.NewFromConfig(cfg)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		_, err = stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
		if err != nil {
			logger.Error().Err(err).Msg("AWS credentials test - invalid credentials")
			c.JSON(http.StatusBadRequest, TestCredentialsResponse{
				Success: false,
				Error:   "Invalid AWS credentials: " + err.Error(),
			})
			return
		}

		logger.Info().Str("region", creds.Region).Msg("AWS credentials test - successful")
		c.JSON(http.StatusOK, TestCredentialsResponse{
			Success: true,
			Message: "AWS credentials are valid",
		})
	}
}

// GetRDSInstances, SQL Server RDS instance'larını getirir
func GetRDSInstances(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req RDSInstancesRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			logger.Error().Err(err).Msg("RDS instances - invalid request format")
			c.JSON(http.StatusBadRequest, RDSInstancesResponse{
				Status: "error",
				Error:  "Invalid request format: " + err.Error(),
			})
			return
		}

		// Kimlik bilgilerini doğrula
		if err := validateAWSCredentials(req.Credentials); err != nil {
			logger.Error().Err(err).Msg("RDS instances - credential validation failed")
			c.JSON(http.StatusBadRequest, RDSInstancesResponse{
				Status: "error",
				Error:  err.Error(),
			})
			return
		}

		// AWS konfigürasyonu oluştur
		cfg, err := createAWSConfig(req.Credentials)
		if err != nil {
			logger.Error().Err(err).Msg("RDS instances - failed to create AWS config")
			c.JSON(http.StatusInternalServerError, RDSInstancesResponse{
				Status: "error",
				Error:  "Failed to create AWS config: " + err.Error(),
			})
			return
		}

		// RDS client oluştur
		rdsClient := rds.NewFromConfig(cfg)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		// Tüm DB instance'larını getir
		result, err := rdsClient.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{})
		if err != nil {
			logger.Error().Err(err).Msg("RDS instances - failed to describe DB instances")
			c.JSON(http.StatusInternalServerError, RDSInstancesResponse{
				Status: "error",
				Error:  "Failed to describe DB instances: " + err.Error(),
			})
			return
		}

		// SQL Server instance'larını filtrele
		var sqlServerInstances []rdstypes.DBInstance
		for _, instance := range result.DBInstances {
			if instance.Engine != nil && strings.HasPrefix(*instance.Engine, "sqlserver") {
				sqlServerInstances = append(sqlServerInstances, instance)
			}
		}

		logger.Info().
			Str("region", req.Region).
			Int("total_instances", len(result.DBInstances)).
			Int("sqlserver_instances", len(sqlServerInstances)).
			Msg("RDS instances retrieved successfully")

		c.JSON(http.StatusOK, RDSInstancesResponse{
			Status:    "success",
			Instances: sqlServerInstances,
		})
	}
}

// GetCloudWatchMetrics, RDS instance için CloudWatch metriklerini getirir
func GetCloudWatchMetrics(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req CloudWatchMetricsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			logger.Error().Err(err).Msg("CloudWatch metrics - invalid request format")
			c.JSON(http.StatusBadRequest, CloudWatchMetricsResponse{
				Status: "error",
				Error:  "Invalid request format: " + err.Error(),
			})
			return
		}

		// Kimlik bilgilerini doğrula
		if err := validateAWSCredentials(req.Credentials); err != nil {
			logger.Error().Err(err).Msg("CloudWatch metrics - credential validation failed")
			c.JSON(http.StatusBadRequest, CloudWatchMetricsResponse{
				Status: "error",
				Error:  err.Error(),
			})
			return
		}

		// Zaman aralığını parse et
		rangeMapping := map[string]time.Duration{
			"10m": 10 * time.Minute,
			"30m": 30 * time.Minute,
			"1h":  1 * time.Hour,
			"6h":  6 * time.Hour,
			"24h": 24 * time.Hour,
			"7d":  7 * 24 * time.Hour,
		}

		duration, exists := rangeMapping[req.Range]
		if !exists {
			duration = 1 * time.Hour // varsayılan
		}

		// AWS konfigürasyonu oluştur
		cfg, err := createAWSConfig(req.Credentials)
		if err != nil {
			logger.Error().Err(err).Msg("CloudWatch metrics - failed to create AWS config")
			c.JSON(http.StatusInternalServerError, CloudWatchMetricsResponse{
				Status: "error",
				Error:  "Failed to create AWS config: " + err.Error(),
			})
			return
		}

		// CloudWatch client oluştur
		cwClient := cloudwatch.NewFromConfig(cfg)

		// Zaman aralığını hesapla
		endTime := time.Now()
		startTime := endTime.Add(-duration)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		// Metrik istatistiklerini getir
		input := &cloudwatch.GetMetricStatisticsInput{
			Namespace:  aws.String("AWS/RDS"),
			MetricName: aws.String(req.MetricName),
			Dimensions: []types.Dimension{
				{
					Name:  aws.String("DBInstanceIdentifier"),
					Value: aws.String(req.InstanceID),
				},
			},
			StartTime:  aws.Time(startTime),
			EndTime:    aws.Time(endTime),
			Period:     aws.Int32(300), // 5 dakika
			Statistics: []types.Statistic{types.StatisticAverage},
		}

		result, err := cwClient.GetMetricStatistics(ctx, input)
		if err != nil {
			logger.Error().Err(err).
				Str("instance_id", req.InstanceID).
				Str("metric_name", req.MetricName).
				Msg("CloudWatch metrics - failed to get metric statistics")
			c.JSON(http.StatusInternalServerError, CloudWatchMetricsResponse{
				Status: "error",
				Error:  "Failed to get metric statistics: " + err.Error(),
			})
			return
		}

		// Veri noktalarını zamana göre sırala
		sort.Slice(result.Datapoints, func(i, j int) bool {
			return result.Datapoints[i].Timestamp.Before(*result.Datapoints[j].Timestamp)
		})

		// Yanıtı formatla
		var dataPoints []MetricDataPoint
		for _, point := range result.Datapoints {
			if point.Average != nil {
				dataPoints = append(dataPoints, MetricDataPoint{
					Timestamp: point.Timestamp.Format(time.RFC3339),
					Value:     *point.Average,
					Unit:      string(point.Unit),
				})
			}
		}

		logger.Info().
			Str("instance_id", req.InstanceID).
			Str("metric_name", req.MetricName).
			Str("range", req.Range).
			Int("data_points", len(dataPoints)).
			Msg("CloudWatch metrics retrieved successfully")

		c.JSON(http.StatusOK, CloudWatchMetricsResponse{
			Status: "success",
			Data:   dataPoints,
		})
	}
}

// SaveRDSMSSQLInfo, AWS RDS SQL Server bilgilerini veritabanına kaydeder
func SaveRDSMSSQLInfo(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req SaveRDSInfoRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			logger.Error().Err(err).Msg("Save RDS info - invalid request format")
			c.JSON(http.StatusBadRequest, SaveRDSInfoResponse{
				Status: "error",
				Error:  "Invalid request format: " + err.Error(),
			})
			return
		}

		// Kimlik bilgilerini doğrula
		if err := validateAWSCredentials(req.Credentials); err != nil {
			logger.Error().Err(err).Msg("Save RDS info - credential validation failed")
			c.JSON(http.StatusBadRequest, SaveRDSInfoResponse{
				Status: "error",
				Error:  err.Error(),
			})
			return
		}

		// AWS Account ID'yi al (eğer verilmemişse)
		if req.AWSAccountID == "" {
			accountID, err := getAWSAccountID(req.Credentials)
			if err != nil {
				logger.Error().Err(err).Msg("Save RDS info - failed to get AWS account ID")
				c.JSON(http.StatusInternalServerError, SaveRDSInfoResponse{
					Status: "error",
					Error:  "Failed to get AWS account ID: " + err.Error(),
				})
				return
			}
			req.AWSAccountID = accountID
		}

		// Hassas bilgileri şifrele
		instanceData := make(map[string]interface{})

		// Instance bilgilerini map'e kopyala
		instanceBytes, err := json.Marshal(req.InstanceInfo)
		if err != nil {
			logger.Error().Err(err).Msg("Save RDS info - failed to marshal instance info")
			c.JSON(http.StatusInternalServerError, SaveRDSInfoResponse{
				Status: "error",
				Error:  "Failed to process instance info: " + err.Error(),
			})
			return
		}

		if err := json.Unmarshal(instanceBytes, &instanceData); err != nil {
			logger.Error().Err(err).Msg("Save RDS info - failed to unmarshal instance info")
			c.JSON(http.StatusInternalServerError, SaveRDSInfoResponse{
				Status: "error",
				Error:  "Failed to process instance info: " + err.Error(),
			})
			return
		}

		// AWS credentials ekle ve şifrele
		awsCredsMap := map[string]interface{}{
			"accessKeyId":     req.Credentials.AccessKeyID,
			"secretAccessKey": req.Credentials.SecretAccessKey,
			"region":          req.Credentials.Region,
		}
		if req.Credentials.SessionToken != "" {
			awsCredsMap["sessionToken"] = req.Credentials.SessionToken
		}
		instanceData["awsCredentials"] = awsCredsMap

		// Credentials'ları şifrele
		if err := EncryptCredentials(instanceData); err != nil {
			logger.Error().Err(err).Msg("Save RDS info - failed to encrypt credentials")
			c.JSON(http.StatusInternalServerError, SaveRDSInfoResponse{
				Status: "error",
				Error:  "Failed to encrypt credentials: " + err.Error(),
			})
			return
		}

		// Metadata ekle
		instanceData["clusterName"] = req.ClusterName
		instanceData["region"] = req.Region
		instanceData["awsAccountId"] = req.AWSAccountID
		instanceData["createdAt"] = time.Now().UTC()

		// RDS bilgilerini veritabanına kaydet
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Güncellenmiş request oluştur
		updatedReq := SaveRDSInfoRequest{
			ClusterName:  req.ClusterName,
			Region:       req.Region,
			InstanceInfo: req.InstanceInfo,
			Credentials:  req.Credentials,
			AWSAccountID: req.AWSAccountID,
		}

		err = saveRDSMSSQLInfoToDatabase(ctx, server.GetDB(), &updatedReq, instanceData)
		if err != nil {
			logger.Error().
				Err(err).
				Str("cluster_name", req.ClusterName).
				Str("region", req.Region).
				Msg("RDS MSSQL bilgileri veritabanına kaydedilemedi")
			c.JSON(http.StatusInternalServerError, SaveRDSInfoResponse{
				Status: "error",
				Error:  "Failed to save RDS info to database: " + err.Error(),
			})
			return
		}

		logger.Info().
			Str("cluster_name", req.ClusterName).
			Str("region", req.Region).
			Str("aws_account_id", req.AWSAccountID).
			Msg("RDS MSSQL bilgileri başarıyla kaydedildi")

		c.JSON(http.StatusOK, SaveRDSInfoResponse{
			Status:  "success",
			Message: "RDS MSSQL information saved successfully",
		})
	}
}

// getAWSAccountID, AWS Account ID'yi STS üzerinden alır
func getAWSAccountID(creds AWSCredentials) (string, error) {
	cfg, err := createAWSConfig(creds)
	if err != nil {
		return "", err
	}

	stsClient := sts.NewFromConfig(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", err
	}

	if result.Account == nil {
		return "", fmt.Errorf("AWS account ID not found")
	}

	return *result.Account, nil
}

// saveRDSMSSQLInfoToDatabase, RDS MSSQL bilgilerini veritabanına kaydeder
func saveRDSMSSQLInfoToDatabase(ctx context.Context, db *sql.DB, req *SaveRDSInfoRequest, instanceData ...map[string]interface{}) error {
	// Önce mevcut kaydı kontrol et
	var existingData []byte
	var id int

	checkQuery := `
		SELECT id, jsondata FROM public.rds_mssql_data 
		WHERE clustername = $1 AND region = $2
		ORDER BY id DESC LIMIT 1
	`

	err := db.QueryRowContext(ctx, checkQuery, req.ClusterName, req.Region).Scan(&id, &existingData)

	// RDS instance bilgilerini map'e dönüştür
	var rdsData map[string]interface{}

	// Eğer şifrelenmiş instance data verilmişse onu kullan
	if len(instanceData) > 0 && instanceData[0] != nil {
		rdsData = instanceData[0]
	} else {
		// Yoksa eski yöntemi kullan
		rdsData = map[string]interface{}{
			"ClusterName":                req.ClusterName,
			"Region":                     req.Region,
			"AWSAccountID":               req.AWSAccountID,
			"DBInstanceIdentifier":       getStringPtr(req.InstanceInfo.DBInstanceIdentifier),
			"DBInstanceClass":            getStringPtr(req.InstanceInfo.DBInstanceClass),
			"Engine":                     getStringPtr(req.InstanceInfo.Engine),
			"EngineVersion":              getStringPtr(req.InstanceInfo.EngineVersion),
			"DBInstanceStatus":           getStringPtr(req.InstanceInfo.DBInstanceStatus),
			"MasterUsername":             getStringPtr(req.InstanceInfo.MasterUsername),
			"AllocatedStorage":           getInt32Ptr(req.InstanceInfo.AllocatedStorage),
			"StorageType":                getStringPtr(req.InstanceInfo.StorageType),
			"StorageEncrypted":           getBoolPtr(req.InstanceInfo.StorageEncrypted),
			"KmsKeyId":                   getStringPtr(req.InstanceInfo.KmsKeyId),
			"MultiAZ":                    getBoolPtr(req.InstanceInfo.MultiAZ),
			"AvailabilityZone":           getStringPtr(req.InstanceInfo.AvailabilityZone),
			"VpcId":                      getVpcId(req.InstanceInfo.DBSubnetGroup),
			"PubliclyAccessible":         getBoolPtr(req.InstanceInfo.PubliclyAccessible),
			"AutoMinorVersionUpgrade":    getBoolPtr(req.InstanceInfo.AutoMinorVersionUpgrade),
			"BackupRetentionPeriod":      getInt32Ptr(req.InstanceInfo.BackupRetentionPeriod),
			"PreferredBackupWindow":      getStringPtr(req.InstanceInfo.PreferredBackupWindow),
			"PreferredMaintenanceWindow": getStringPtr(req.InstanceInfo.PreferredMaintenanceWindow),
			"LatestRestorableTime":       getTimePtr(req.InstanceInfo.LatestRestorableTime),
			"DeletionProtection":         getBoolPtr(req.InstanceInfo.DeletionProtection),
			"PerformanceInsightsEnabled": getBoolPtr(req.InstanceInfo.PerformanceInsightsEnabled),
			"MonitoringInterval":         getInt32Ptr(req.InstanceInfo.MonitoringInterval),
			"LastUpdated":                time.Now().UTC(),
		}
	}

	// Endpoint bilgilerini ekle
	if req.InstanceInfo.Endpoint != nil {
		endpoint := map[string]interface{}{
			"Address": getStringPtr(req.InstanceInfo.Endpoint.Address),
			"Port":    getInt32Ptr(req.InstanceInfo.Endpoint.Port),
		}
		rdsData["Endpoint"] = endpoint
	}

	// VPC Security Groups bilgilerini ekle
	if len(req.InstanceInfo.VpcSecurityGroups) > 0 {
		securityGroups := make([]map[string]interface{}, len(req.InstanceInfo.VpcSecurityGroups))
		for i, sg := range req.InstanceInfo.VpcSecurityGroups {
			securityGroups[i] = map[string]interface{}{
				"VpcSecurityGroupId": getStringPtr(sg.VpcSecurityGroupId),
				"Status":             getStringPtr(sg.Status),
			}
		}
		rdsData["VpcSecurityGroups"] = securityGroups
	}

	// DB Parameter Groups bilgilerini ekle
	if len(req.InstanceInfo.DBParameterGroups) > 0 {
		paramGroups := make([]map[string]interface{}, len(req.InstanceInfo.DBParameterGroups))
		for i, pg := range req.InstanceInfo.DBParameterGroups {
			paramGroups[i] = map[string]interface{}{
				"DBParameterGroupName": getStringPtr(pg.DBParameterGroupName),
				"ParameterApplyStatus": getStringPtr(pg.ParameterApplyStatus),
			}
		}
		rdsData["DBParameterGroups"] = paramGroups
	}

	var jsonData []byte

	if err == nil {
		// Mevcut kayıt var, güncelle
		logger.Debug().
			Str("cluster_name", req.ClusterName).
			Str("region", req.Region).
			Int("id", id).
			Msg("RDS MSSQL için mevcut kayıt bulundu, güncelleniyor")

		var existingJSON map[string][]interface{}
		if err := json.Unmarshal(existingData, &existingJSON); err != nil {
			logger.Error().
				Err(err).
				Str("cluster_name", req.ClusterName).
				Msg("RDS MSSQL mevcut JSON ayrıştırma hatası")
			return err
		}

		// Cluster array'ini al
		clusterData, ok := existingJSON[req.ClusterName]
		if !ok {
			clusterData = []interface{}{}
		}

		// Instance'ı bul ve güncelle
		instanceFound := false
		instanceChanged := false
		instanceID := getStringPtr(req.InstanceInfo.DBInstanceIdentifier)

		for i, instance := range clusterData {
			instanceMap, ok := instance.(map[string]interface{})
			if !ok {
				continue
			}

			// DBInstanceIdentifier ile instance eşleşmesi kontrol et
			if instanceMap["DBInstanceIdentifier"] == instanceID {
				instanceFound = true

				// Değişiklikleri takip et
				for key, newValue := range rdsData {
					currentValue, exists := instanceMap[key]
					var hasChanged bool

					if !exists {
						hasChanged = true
						logger.Debug().
							Str("instance_id", instanceID).
							Str("field", key).
							Msg("RDS instance'da yeni alan eklendi")
					} else {
						hasChanged = !compareValues(currentValue, newValue)
						if hasChanged {
							logger.Debug().
								Str("instance_id", instanceID).
								Str("field", key).
								Interface("old_value", currentValue).
								Interface("new_value", newValue).
								Msg("RDS instance'da değişiklik tespit edildi")
						}
					}

					if hasChanged {
						instanceMap[key] = newValue
						// Önemli değişiklikleri işaretle
						if key == "DBInstanceStatus" || key == "EngineVersion" || key == "AllocatedStorage" || key == "MultiAZ" {
							instanceChanged = true
						}
					}
				}

				clusterData[i] = instanceMap
				break
			}
		}

		// Eğer instance bulunamadıysa yeni ekle
		if !instanceFound {
			clusterData = append(clusterData, rdsData)
			instanceChanged = true
			logger.Info().
				Str("instance_id", instanceID).
				Str("cluster_name", req.ClusterName).
				Msg("Yeni RDS MSSQL instance eklendi")
		}

		// Eğer önemli bir değişiklik yoksa veritabanını güncelleme
		if !instanceChanged {
			logger.Debug().
				Str("instance_id", instanceID).
				Str("cluster_name", req.ClusterName).
				Msg("RDS instance'da önemli bir değişiklik yok, güncelleme yapılmadı")
			return nil
		}

		existingJSON[req.ClusterName] = clusterData

		// JSON'ı güncelle
		jsonData, err = json.Marshal(existingJSON)
		if err != nil {
			logger.Error().
				Err(err).
				Str("cluster_name", req.ClusterName).
				Msg("RDS MSSQL JSON dönüştürme hatası")
			return err
		}

		// Veritabanını güncelle
		updateQuery := `
			UPDATE public.rds_mssql_data 
			SET jsondata = $1, updated_at = CURRENT_TIMESTAMP
			WHERE id = $2
		`

		_, err = db.ExecContext(ctx, updateQuery, jsonData, id)
		if err != nil {
			logger.Error().
				Err(err).
				Str("cluster_name", req.ClusterName).
				Msg("RDS MSSQL veritabanı güncelleme hatası")
			return err
		}

		logger.Info().
			Str("cluster_name", req.ClusterName).
			Str("region", req.Region).
			Msg("RDS MSSQL kaydı güncellendi")

	} else if err == sql.ErrNoRows {
		// Yeni kayıt oluştur
		logger.Debug().
			Str("cluster_name", req.ClusterName).
			Str("region", req.Region).
			Msg("RDS MSSQL için yeni kayıt oluşturuluyor")

		newJSON := map[string][]interface{}{
			req.ClusterName: {rdsData},
		}

		jsonData, err = json.Marshal(newJSON)
		if err != nil {
			logger.Error().
				Err(err).
				Str("cluster_name", req.ClusterName).
				Msg("RDS MSSQL yeni JSON dönüştürme hatası")
			return err
		}

		// Yeni kayıt ekle
		insertQuery := `
			INSERT INTO public.rds_mssql_data (jsondata, clustername, region, aws_account_id, created_at, updated_at)
			VALUES ($1, $2, $3, $4, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		`

		_, err = db.ExecContext(ctx, insertQuery, jsonData, req.ClusterName, req.Region, req.AWSAccountID)
		if err != nil {
			logger.Error().
				Err(err).
				Str("cluster_name", req.ClusterName).
				Msg("RDS MSSQL yeni kayıt ekleme hatası")
			return err
		}

		logger.Info().
			Str("cluster_name", req.ClusterName).
			Str("region", req.Region).
			Msg("Yeni RDS MSSQL kaydı oluşturuldu")

	} else {
		// Diğer veritabanı hataları
		logger.Error().
			Err(err).
			Str("cluster_name", req.ClusterName).
			Msg("RDS MSSQL veritabanı sorgulama hatası")
		return err
	}

	return nil
}

// Helper functions for pointer handling
func getStringPtr(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}

func getInt32Ptr(ptr *int32) int32 {
	if ptr == nil {
		return 0
	}
	return *ptr
}

func getBoolPtr(ptr *bool) bool {
	if ptr == nil {
		return false
	}
	return *ptr
}

func getTimePtr(ptr *time.Time) *time.Time {
	return ptr
}

func getVpcId(subnetGroup *rdstypes.DBSubnetGroup) string {
	if subnetGroup == nil {
		return ""
	}
	return getStringPtr(subnetGroup.VpcId)
}

// compareValues, iki değeri karşılaştırır (mevcut MSSQL kodundan alınmış)
func compareValues(oldValue, newValue interface{}) bool {
	// Nil kontrolü
	if oldValue == nil && newValue == nil {
		return true
	}
	if oldValue == nil || newValue == nil {
		return false
	}

	// Time karşılaştırması
	if oldTime, ok := oldValue.(time.Time); ok {
		if newTime, ok := newValue.(time.Time); ok {
			return oldTime.Equal(newTime)
		}
	}

	// Diğer türler için basit karşılaştırma
	return oldValue == newValue
}

// GetSavedRDSInstances, kayıtlı RDS instance'larını veritabanından getirir
func GetSavedRDSInstances(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		query := `
			SELECT id, clustername, region, jsondata, aws_account_id, created_at, updated_at
			FROM public.rds_mssql_data
			ORDER BY created_at DESC
		`

		rows, err := server.GetDB().QueryContext(ctx, query)
		if err != nil {
			logger.Error().Err(err).Msg("Failed to fetch RDS instances from database")
			c.JSON(http.StatusInternalServerError, GetRDSInstancesResponse{
				Success: false,
				Error:   "Failed to fetch RDS instances: " + err.Error(),
			})
			return
		}
		defer rows.Close()

		var instances []map[string]interface{}

		for rows.Next() {
			var id int
			var clustername, region string
			var jsonData string
			var awsAccountID sql.NullString
			var createdAt, updatedAt time.Time

			err := rows.Scan(&id, &clustername, &region, &jsonData, &awsAccountID, &createdAt, &updatedAt)
			if err != nil {
				logger.Error().Err(err).Msg("Failed to scan RDS instance row")
				continue
			}

			// JSON verisini parse et
			var data map[string]interface{}
			if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
				logger.Error().Err(err).Str("cluster_name", clustername).Msg("Failed to parse JSON data")
				continue
			}

			// Şifreli bilgileri çöz
			if err := DecryptCredentials(data); err != nil {
				logger.Error().Err(err).Str("cluster_name", clustername).Msg("Failed to decrypt credentials")
				// Şifre çözme hatası durumunda hassas bilgileri temizle
				if awsCreds, ok := data["awsCredentials"].(map[string]interface{}); ok {
					awsCreds["secretAccessKey"] = "[DECRYPTION_ERROR]"
					awsCreds["sessionToken"] = "[DECRYPTION_ERROR]"
				}
				if sqlCreds, ok := data["sqlCredentials"].(map[string]interface{}); ok {
					sqlCreds["password"] = "[DECRYPTION_ERROR]"
				}
			}

			var awsAccountIDStr string
			if awsAccountID.Valid {
				awsAccountIDStr = awsAccountID.String
			}

			instances = append(instances, map[string]interface{}{
				"id":             id,
				"clustername":    clustername,
				"region":         region,
				"jsondata":       data,
				"aws_account_id": awsAccountIDStr,
				"created_at":     createdAt,
				"updated_at":     updatedAt,
			})
		}

		if err = rows.Err(); err != nil {
			logger.Error().Err(err).Msg("Error iterating RDS instance rows")
			c.JSON(http.StatusInternalServerError, GetRDSInstancesResponse{
				Success: false,
				Error:   "Error processing RDS instances: " + err.Error(),
			})
			return
		}

		logger.Info().Int("count", len(instances)).Msg("Retrieved saved RDS instances")

		c.JSON(http.StatusOK, GetRDSInstancesResponse{
			Success: true,
			Data:    instances,
		})
	}
}

// TestSQLConnection, SQL Server bağlantısını test eder
func TestSQLConnection(server *server.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req SQLConnectionRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			logger.Error().Err(err).Msg("SQL connection test - invalid request format")
			c.JSON(http.StatusBadRequest, SQLConnectionResponse{
				Success: false,
				Error:   "Invalid request format: " + err.Error(),
			})
			return
		}

		// Bağlantı string'ini oluştur
		connString := fmt.Sprintf("server=%s;port=%d;database=%s;user id=%s;password=%s;encrypt=true;trustservercertificate=true;connection timeout=30",
			req.Endpoint, req.Port, req.Database, req.Username, req.Password)

		logger.Debug().
			Str("endpoint", req.Endpoint).
			Int("port", req.Port).
			Str("database", req.Database).
			Str("username", req.Username).
			Msg("Testing SQL Server connection")

		// Bağlantıyı test et
		db, err := sql.Open("sqlserver", connString)
		if err != nil {
			logger.Error().Err(err).Msg("Failed to create SQL Server connection")
			c.JSON(http.StatusBadRequest, SQLConnectionResponse{
				Success: false,
				Error:   "Failed to create connection: " + err.Error(),
			})
			return
		}
		defer db.Close()

		// Bağlantı timeout'u ayarla
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Basit bir sorgu ile bağlantıyı test et
		err = db.PingContext(ctx)
		if err != nil {
			logger.Error().Err(err).Msg("SQL Server connection test failed")
			c.JSON(http.StatusBadRequest, SQLConnectionResponse{
				Success: false,
				Error:   "Connection test failed: " + err.Error(),
			})
			return
		}

		// Ek test: basit bir SELECT sorgusu çalıştır
		var version string
		err = db.QueryRowContext(ctx, "SELECT @@VERSION").Scan(&version)
		if err != nil {
			logger.Error().Err(err).Msg("Failed to query SQL Server version")
			c.JSON(http.StatusBadRequest, SQLConnectionResponse{
				Success: false,
				Error:   "Failed to query server: " + err.Error(),
			})
			return
		}

		logger.Info().
			Str("endpoint", req.Endpoint).
			Str("database", req.Database).
			Str("version", version[:50]). // İlk 50 karakter
			Msg("SQL Server connection test successful")

		c.JSON(http.StatusOK, SQLConnectionResponse{
			Success: true,
			Message: "Connection successful",
		})
	}
}
