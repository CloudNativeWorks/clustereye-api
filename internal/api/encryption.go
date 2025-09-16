package api

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"

	"github.com/sefaphlvn/clustereye-test/internal/logger"
)

var encryptionKey []byte

// InitEncryption, şifreleme anahtarını başlatır
func InitEncryption() error {
	keyStr := os.Getenv("ENCRYPTION_KEY")
	if keyStr == "" {
		// Geliştirme ortamı için varsayılan anahtar (ÜRETİMDE MUTLAKA DEĞİŞTİRİLMELİ!)
		keyStr = "clustereye-default-32byte-key!!!"
		logger.Warn().Msg("ENCRYPTION_KEY environment variable not set, using default key (NOT SECURE FOR PRODUCTION)")
	}

	if len(keyStr) != 32 {
		return fmt.Errorf("encryption key must be exactly 32 bytes, got %d bytes", len(keyStr))
	}

	encryptionKey = []byte(keyStr)
	logger.Info().Msg("Encryption initialized successfully")
	return nil
}

// EncryptString, verilen string'i AES-GCM ile şifreler
func EncryptString(plaintext string) (string, error) {
	if len(encryptionKey) == 0 {
		return "", fmt.Errorf("encryption not initialized")
	}

	if plaintext == "" {
		return "", nil
	}

	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to create AES cipher")
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to create GCM")
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		logger.Error().Err(err).Msg("Failed to generate nonce")
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	encoded := base64.StdEncoding.EncodeToString(ciphertext)

	logger.Debug().Msg("String encrypted successfully")
	return encoded, nil
}

// DecryptString, verilen şifreli string'i çözer
func DecryptString(ciphertext string) (string, error) {
	if len(encryptionKey) == 0 {
		return "", fmt.Errorf("encryption not initialized")
	}

	if ciphertext == "" {
		return "", nil
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to decode base64")
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}

	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to create AES cipher")
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to create GCM")
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, cipherData := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, cipherData, nil)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to decrypt data")
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	logger.Debug().Msg("String decrypted successfully")
	return string(plaintext), nil
}

// EncryptCredentials, credentials map'indeki hassas bilgileri şifreler
func EncryptCredentials(creds map[string]interface{}) error {
	// AWS credentials şifreleme
	if awsCreds, ok := creds["awsCredentials"].(map[string]interface{}); ok {
		if secretKey, exists := awsCreds["secretAccessKey"].(string); exists && secretKey != "" {
			encrypted, err := EncryptString(secretKey)
			if err != nil {
				return fmt.Errorf("failed to encrypt AWS secret key: %w", err)
			}
			awsCreds["secretAccessKey"] = encrypted
		}

		if sessionToken, exists := awsCreds["sessionToken"].(string); exists && sessionToken != "" {
			encrypted, err := EncryptString(sessionToken)
			if err != nil {
				return fmt.Errorf("failed to encrypt AWS session token: %w", err)
			}
			awsCreds["sessionToken"] = encrypted
		}
	}

	// SQL credentials şifreleme
	if sqlCreds, ok := creds["sqlCredentials"].(map[string]interface{}); ok {
		if password, exists := sqlCreds["password"].(string); exists && password != "" {
			encrypted, err := EncryptString(password)
			if err != nil {
				return fmt.Errorf("failed to encrypt SQL password: %w", err)
			}
			sqlCreds["password"] = encrypted
		}
	}

	return nil
}

// DecryptCredentials, credentials map'indeki şifreli bilgileri çözer
func DecryptCredentials(creds map[string]interface{}) error {
	// AWS credentials çözme
	if awsCreds, ok := creds["awsCredentials"].(map[string]interface{}); ok {
		if secretKey, exists := awsCreds["secretAccessKey"].(string); exists && secretKey != "" {
			decrypted, err := DecryptString(secretKey)
			if err != nil {
				return fmt.Errorf("failed to decrypt AWS secret key: %w", err)
			}
			awsCreds["secretAccessKey"] = decrypted
		}

		if sessionToken, exists := awsCreds["sessionToken"].(string); exists && sessionToken != "" {
			decrypted, err := DecryptString(sessionToken)
			if err != nil {
				return fmt.Errorf("failed to decrypt AWS session token: %w", err)
			}
			awsCreds["sessionToken"] = decrypted
		}
	}

	// SQL credentials çözme
	if sqlCreds, ok := creds["sqlCredentials"].(map[string]interface{}); ok {
		if password, exists := sqlCreds["password"].(string); exists && password != "" {
			decrypted, err := DecryptString(password)
			if err != nil {
				return fmt.Errorf("failed to decrypt SQL password: %w", err)
			}
			sqlCreds["password"] = decrypted
		}
	}

	return nil
}
