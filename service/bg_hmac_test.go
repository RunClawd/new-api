package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHMACSignatureCorrectness(t *testing.T) {
	secret := "test_secret_123"
	payload := `{"event":"response_created","data":{"id":"1"}}`

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	assert.NotEmpty(t, expectedSignature)
	assert.Equal(t, "9f43006d6d0f383a9b3593f71943abfb5e277d310cccd5d4f78fe71fe7a594a5", expectedSignature)
}
