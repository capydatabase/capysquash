package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
)

// WebhookHandler verifies and acknowledges GitHub webhook deliveries.
//
// It performs HMAC-SHA256 signature verification against the configured
// webhook secret and acknowledges valid deliveries. Event processing (PR
// analysis, check runs, comments) is performed by consumers such as
// capysquash-api through AppClient/InstallationClient, not by this receiver.
type WebhookHandler struct {
	secret string
}

// NewWebhookHandler creates a webhook handler that verifies deliveries with
// the given shared secret. The secret must be non-empty; verification with an
// empty HMAC key would accept forged payloads.
func NewWebhookHandler(secret string) *WebhookHandler {
	return &WebhookHandler{secret: secret}
}

// HandleWebhook verifies the delivery signature and acknowledges the event.
// Invalid signatures are rejected with 401.
func (h *WebhookHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	if !h.verifySignature(r.Header.Get("X-Hub-Signature-256"), body) {
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// verifySignature validates the webhook payload against the X-Hub-Signature-256 header.
func (h *WebhookHandler) verifySignature(signature string, body []byte) bool {
	if signature == "" || h.secret == "" {
		return false
	}

	mac := hmac.New(sha256.New, []byte(h.secret))
	mac.Write(body)
	expectedMAC := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(signature), []byte(expectedMAC))
}
