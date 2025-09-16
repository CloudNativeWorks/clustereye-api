package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/gin-gonic/gin"
)

func TestValidateAWSCredentials(t *testing.T) {
	tests := []struct {
		name        string
		credentials AWSCredentials
		wantErr     bool
		errContains string
	}{
		{
			name: "valid credentials",
			credentials: AWSCredentials{
				AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
				SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
				Region:          "us-east-1",
			},
			wantErr: false,
		},
		{
			name: "missing access key",
			credentials: AWSCredentials{
				SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
				Region:          "us-east-1",
			},
			wantErr:     true,
			errContains: "accessKeyId",
		},
		{
			name: "missing secret key",
			credentials: AWSCredentials{
				AccessKeyID: "AKIAIOSFODNN7EXAMPLE",
				Region:      "us-east-1",
			},
			wantErr:     true,
			errContains: "secretAccessKey",
		},
		{
			name: "missing region",
			credentials: AWSCredentials{
				AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
				SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			},
			wantErr:     true,
			errContains: "region",
		},
		{
			name: "invalid access key format",
			credentials: AWSCredentials{
				AccessKeyID:     "INVALIDKEY",
				SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
				Region:          "us-east-1",
			},
			wantErr:     true,
			errContains: "Access Key ID format",
		},
		{
			name: "invalid secret key length",
			credentials: AWSCredentials{
				AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
				SecretAccessKey: "tooshort",
				Region:          "us-east-1",
			},
			wantErr:     true,
			errContains: "Secret Access Key format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAWSCredentials(tt.credentials)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateAWSCredentials() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" {
				if err == nil || !contains(err.Error(), tt.errContains) {
					t.Errorf("validateAWSCredentials() error = %v, want error containing %v", err, tt.errContains)
				}
			}
		})
	}
}

func TestAWSRateLimiter(t *testing.T) {
	limiter := NewAWSRateLimiter(2, time.Minute) // 2 requests per minute

	// İlk iki istek geçmeli
	if !limiter.IsAllowed("127.0.0.1") {
		t.Error("First request should be allowed")
	}
	if !limiter.IsAllowed("127.0.0.1") {
		t.Error("Second request should be allowed")
	}

	// Üçüncü istek reddedilmeli
	if limiter.IsAllowed("127.0.0.1") {
		t.Error("Third request should be denied")
	}

	// Farklı IP için istek geçmeli
	if !limiter.IsAllowed("192.168.1.1") {
		t.Error("Request from different IP should be allowed")
	}
}

func TestAWSSecurityMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(AWSSecurityMiddleware())
	router.POST("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	tests := []struct {
		name           string
		contentType    string
		body           string
		expectedStatus int
	}{
		{
			name:           "valid request",
			contentType:    "application/json",
			body:           `{"test": "data"}`,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid content type",
			contentType:    "text/plain",
			body:           `test data`,
			expectedStatus: http.StatusUnsupportedMediaType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", tt.contentType)

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

func TestSaveRDSInfoRequest(t *testing.T) {
	// Mock RDS instance data
	mockInstance := rdstypes.DBInstance{
		DBInstanceIdentifier: aws.String("test-sql-server"),
		DBInstanceClass:      aws.String("db.t3.medium"),
		Engine:               aws.String("sqlserver-se"),
		EngineVersion:        aws.String("15.00.4073.23.v1"),
		DBInstanceStatus:     aws.String("available"),
		MasterUsername:       aws.String("admin"),
		AllocatedStorage:     aws.Int32(100),
		StorageType:          aws.String("gp2"),
		MultiAZ:              aws.Bool(false),
		AvailabilityZone:     aws.String("us-east-1a"),
		PubliclyAccessible:   aws.Bool(false),
		StorageEncrypted:     aws.Bool(true),
	}

	tests := []struct {
		name        string
		request     SaveRDSInfoRequest
		wantErr     bool
		errContains string
	}{
		{
			name: "valid request",
			request: SaveRDSInfoRequest{
				ClusterName:  "test-cluster",
				Region:       "us-east-1",
				InstanceInfo: mockInstance,
				Credentials: AWSCredentials{
					AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
					SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
					Region:          "us-east-1",
				},
				AWSAccountID: "123456789012",
			},
			wantErr: false,
		},
		{
			name: "missing cluster name",
			request: SaveRDSInfoRequest{
				Region:       "us-east-1",
				InstanceInfo: mockInstance,
				Credentials: AWSCredentials{
					AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
					SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
					Region:          "us-east-1",
				},
			},
			wantErr: false, // validateAWSCredentials sadece credentials'ı kontrol eder
		},
		{
			name: "invalid credentials",
			request: SaveRDSInfoRequest{
				ClusterName:  "test-cluster",
				Region:       "us-east-1",
				InstanceInfo: mockInstance,
				Credentials: AWSCredentials{
					AccessKeyID:     "INVALID",
					SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
					Region:          "us-east-1",
				},
			},
			wantErr:     true,
			errContains: "Access Key ID format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAWSCredentials(tt.request.Credentials)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateAWSCredentials() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" {
				if err == nil || !contains(err.Error(), tt.errContains) {
					t.Errorf("validateAWSCredentials() error = %v, want error containing %v", err, tt.errContains)
				}
			}
		})
	}
}

func TestHelperFunctions(t *testing.T) {
	// Test getStringPtr
	testStr := "test"
	if getStringPtr(&testStr) != "test" {
		t.Error("getStringPtr failed for non-nil string")
	}
	if getStringPtr(nil) != "" {
		t.Error("getStringPtr failed for nil string")
	}

	// Test getInt32Ptr
	testInt := int32(42)
	if getInt32Ptr(&testInt) != 42 {
		t.Error("getInt32Ptr failed for non-nil int")
	}
	if getInt32Ptr(nil) != 0 {
		t.Error("getInt32Ptr failed for nil int")
	}

	// Test getBoolPtr
	testBool := true
	if getBoolPtr(&testBool) != true {
		t.Error("getBoolPtr failed for non-nil bool")
	}
	if getBoolPtr(nil) != false {
		t.Error("getBoolPtr failed for nil bool")
	}
}

func TestCompareValues(t *testing.T) {
	// Test nil values
	if !compareValues(nil, nil) {
		t.Error("compareValues failed for both nil")
	}
	if compareValues(nil, "test") {
		t.Error("compareValues failed for nil vs non-nil")
	}

	// Test same values
	if !compareValues("test", "test") {
		t.Error("compareValues failed for same strings")
	}
	if !compareValues(42, 42) {
		t.Error("compareValues failed for same integers")
	}

	// Test different values
	if compareValues("test1", "test2") {
		t.Error("compareValues failed for different strings")
	}

	// Test time values
	now := time.Now()
	later := now.Add(time.Hour)
	if !compareValues(now, now) {
		t.Error("compareValues failed for same time")
	}
	if compareValues(now, later) {
		t.Error("compareValues failed for different times")
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > len(substr) && (s[:len(substr)] == substr ||
			s[len(s)-len(substr):] == substr ||
			findSubstring(s, substr))))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestEncryptionFunctions(t *testing.T) {
	// Initialize encryption for testing
	err := InitEncryption()
	if err != nil {
		t.Fatalf("Failed to initialize encryption: %v", err)
	}

	tests := []struct {
		name      string
		plaintext string
	}{
		{"empty string", ""},
		{"simple text", "hello world"},
		{"special chars", "password123!@#$%^&*()"},
		{"long text", "this is a very long password with many characters and symbols !@#$%^&*()1234567890"},
		{"unicode", "şifre123üğıöç"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test encryption
			encrypted, err := EncryptString(tt.plaintext)
			if err != nil {
				t.Errorf("EncryptString() error = %v", err)
				return
			}

			// Empty string should return empty
			if tt.plaintext == "" && encrypted != "" {
				t.Errorf("EncryptString() for empty string should return empty, got %s", encrypted)
				return
			}

			if tt.plaintext == "" {
				return // Skip decryption test for empty string
			}

			// Test decryption
			decrypted, err := DecryptString(encrypted)
			if err != nil {
				t.Errorf("DecryptString() error = %v", err)
				return
			}

			if decrypted != tt.plaintext {
				t.Errorf("DecryptString() = %v, want %v", decrypted, tt.plaintext)
			}

			// Encrypted should be different from plaintext (unless empty)
			if encrypted == tt.plaintext && tt.plaintext != "" {
				t.Errorf("Encrypted text should be different from plaintext")
			}
		})
	}
}

func TestEncryptCredentials(t *testing.T) {
	// Initialize encryption for testing
	err := InitEncryption()
	if err != nil {
		t.Fatalf("Failed to initialize encryption: %v", err)
	}

	creds := map[string]interface{}{
		"awsCredentials": map[string]interface{}{
			"accessKeyId":     "AKIAIOSFODNN7EXAMPLE",
			"secretAccessKey": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			"region":          "us-east-1",
			"sessionToken":    "session-token-123",
		},
		"sqlCredentials": map[string]interface{}{
			"username": "admin",
			"password": "super-secret-password",
		},
	}

	// Make a copy for comparison
	originalAWSSecret := creds["awsCredentials"].(map[string]interface{})["secretAccessKey"].(string)
	originalSessionToken := creds["awsCredentials"].(map[string]interface{})["sessionToken"].(string)
	originalSQLPassword := creds["sqlCredentials"].(map[string]interface{})["password"].(string)

	// Encrypt credentials
	err = EncryptCredentials(creds)
	if err != nil {
		t.Fatalf("EncryptCredentials() error = %v", err)
	}

	// Check that secrets are encrypted (different from original)
	encryptedAWSSecret := creds["awsCredentials"].(map[string]interface{})["secretAccessKey"].(string)
	encryptedSessionToken := creds["awsCredentials"].(map[string]interface{})["sessionToken"].(string)
	encryptedSQLPassword := creds["sqlCredentials"].(map[string]interface{})["password"].(string)

	if encryptedAWSSecret == originalAWSSecret {
		t.Error("AWS secret key should be encrypted")
	}
	if encryptedSessionToken == originalSessionToken {
		t.Error("AWS session token should be encrypted")
	}
	if encryptedSQLPassword == originalSQLPassword {
		t.Error("SQL password should be encrypted")
	}

	// Decrypt credentials
	err = DecryptCredentials(creds)
	if err != nil {
		t.Fatalf("DecryptCredentials() error = %v", err)
	}

	// Check that secrets are decrypted back to original
	decryptedAWSSecret := creds["awsCredentials"].(map[string]interface{})["secretAccessKey"].(string)
	decryptedSessionToken := creds["awsCredentials"].(map[string]interface{})["sessionToken"].(string)
	decryptedSQLPassword := creds["sqlCredentials"].(map[string]interface{})["password"].(string)

	if decryptedAWSSecret != originalAWSSecret {
		t.Errorf("Decrypted AWS secret = %v, want %v", decryptedAWSSecret, originalAWSSecret)
	}
	if decryptedSessionToken != originalSessionToken {
		t.Errorf("Decrypted session token = %v, want %v", decryptedSessionToken, originalSessionToken)
	}
	if decryptedSQLPassword != originalSQLPassword {
		t.Errorf("Decrypted SQL password = %v, want %v", decryptedSQLPassword, originalSQLPassword)
	}
}
