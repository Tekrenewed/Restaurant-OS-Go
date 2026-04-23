package aggregator

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// VerifySignature checks if the incoming webhook payload has a valid HMAC SHA256 signature.
// Providers like Uber Eats and Just Eat send their payloads hashed with a shared secret.
func VerifySignature(secret string, payload []byte, signatureHeader string) bool {
	if secret == "" || signatureHeader == "" {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expectedMAC := hex.EncodeToString(mac.Sum(nil))

	// Some providers prefix with "sha256=" or similar, we should check equality or substring securely.
	// We'll use secure compare to prevent timing attacks.
	return hmac.Equal([]byte(expectedMAC), []byte(signatureHeader))
}
