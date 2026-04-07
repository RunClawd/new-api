package adapters

import (
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// CreateKlingJWT generates a short-lived JWT for the Kling API.
// apiKey must be in "access_key|secret_key" format.
// If apiKey starts with "sk-", it is treated as a relay-style bearer token
// and returned as-is (no JWT signing needed).
func CreateKlingJWT(apiKey string) (string, error) {
	if strings.HasPrefix(apiKey, "sk-") {
		return apiKey, nil // relay-style API key
	}

	parts := strings.Split(apiKey, "|")
	if len(parts) != 2 {
		return "", errors.New("invalid kling api_key, expected format: access_key|secret_key")
	}
	accessKey := strings.TrimSpace(parts[0])
	secretKey := strings.TrimSpace(parts[1])

	now := time.Now().Unix()
	claims := jwt.MapClaims{
		"iss": accessKey,
		"exp": now + 1800, // 30 minutes
		"nbf": now - 5,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["typ"] = "JWT"
	return token.SignedString([]byte(secretKey))
}
