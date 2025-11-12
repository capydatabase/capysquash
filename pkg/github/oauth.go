package github

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/capysquash/pgsquash-engine/internal/errors"
)

// OAuthHandler handles GitHub OAuth flow with CSRF protection
type OAuthHandler struct {
    clientID     string
    clientSecret string
    redirectURL  string
    states       map[string]stateEntry // CSRF state tokens
    stateMutex   sync.RWMutex
}

// stateEntry tracks CSRF state tokens with expiration
type stateEntry struct {
    createdAt time.Time
    expiresAt time.Time
}

// NewOAuthHandler creates a new OAuth handler with CSRF protection
func NewOAuthHandler(clientID, clientSecret, redirectURL string) *OAuthHandler {
    handler := &OAuthHandler{
        clientID:     clientID,
        clientSecret: clientSecret,
        redirectURL:  redirectURL,
        states:       make(map[string]stateEntry),
    }

    // Start background cleanup of expired states
    go handler.cleanupExpiredStates()

    return handler
}

// GenerateState generates a cryptographically secure CSRF state token
func (h *OAuthHandler) GenerateState() (string, error) {
    b := make([]byte, 32)
    if _, err := rand.Read(b); err != nil {
        return "", errors.NewError(
            errors.ErrorCodeValidationFailed,
            "failed to generate random state token",
            errors.SeverityError,
            errors.CategoryValidation,
        ).WithInnerError(err).WithSuggestion("Ensure system entropy source is available")
    }

    state := base64.URLEncoding.EncodeToString(b)

    // Store state with 10-minute expiration
    h.stateMutex.Lock()
    h.states[state] = stateEntry{
        createdAt: time.Now(),
        expiresAt: time.Now().Add(10 * time.Minute),
    }
    h.stateMutex.Unlock()

    return state, nil
}

// ValidateState validates and consumes a CSRF state token
func (h *OAuthHandler) ValidateState(state string) bool {
    h.stateMutex.Lock()
    defer h.stateMutex.Unlock()

    entry, exists := h.states[state]
    if !exists {
        return false
    }

    // Check if expired
    if time.Now().After(entry.expiresAt) {
        delete(h.states, state)
        return false
    }

    // Consume the state token (one-time use)
    delete(h.states, state)
    return true
}

// cleanupExpiredStates removes expired state tokens every minute
func (h *OAuthHandler) cleanupExpiredStates() {
    ticker := time.NewTicker(1 * time.Minute)
    defer ticker.Stop()

    for range ticker.C {
        h.stateMutex.Lock()
        now := time.Now()
        for state, entry := range h.states {
            if now.After(entry.expiresAt) {
                delete(h.states, state)
            }
        }
        h.stateMutex.Unlock()
    }
}

// GetAuthorizationURL returns the GitHub OAuth authorization URL with CSRF protection
func (h *OAuthHandler) GetAuthorizationURL(state string) string {
    params := url.Values{
        "client_id":    {h.clientID},
        "redirect_uri": {h.redirectURL},
        "scope":        {"repo,write:repo_hook"},
        "state":        {state},
    }

    return "https://github.com/login/oauth/authorize?" + params.Encode()
}

// ExchangeCodeForToken exchanges an authorization code for an access token
func (h *OAuthHandler) ExchangeCodeForToken(ctx context.Context, code string) (string, error) {
    data := url.Values{
        "client_id":     {h.clientID},
        "client_secret": {h.clientSecret},
        "code":          {code},
        "redirect_uri":  {h.redirectURL},
    }

    req, err := http.NewRequestWithContext(ctx, "POST", "https://github.com/login/oauth/access_token",
        strings.NewReader(data.Encode()))
    if err != nil {
        return "", err
    }

    req.Header.Set("Accept", "application/json")
    req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        return "", err
    }
    defer func() { _ = resp.Body.Close() }()

    var result struct {
        AccessToken string `json:"access_token"`
        TokenType   string `json:"token_type"`
        Scope       string `json:"scope"`
    }

    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return "", err
    }

    if result.AccessToken == "" {
        return "", errors.NewError(
            errors.ErrorCodeValidationFailed,
            "no access token received from GitHub",
            errors.SeverityError,
            errors.CategoryValidation,
        ).WithSuggestion("Check OAuth app configuration and authorization")
    }

    return result.AccessToken, nil
}

// HandleCallback handles the OAuth callback with CSRF validation
func (h *OAuthHandler) HandleCallback(w http.ResponseWriter, r *http.Request) {
    code := r.URL.Query().Get("code")
    state := r.URL.Query().Get("state")

    if code == "" {
        http.Error(w, "No code provided", http.StatusBadRequest)
        return
    }

    if state == "" {
        http.Error(w, "No state provided", http.StatusBadRequest)
        return
    }

    // Validate CSRF state token
    if !h.ValidateState(state) {
        http.Error(w, "Invalid or expired CSRF state token", http.StatusUnauthorized)
        return
    }

    token, err := h.ExchangeCodeForToken(r.Context(), code)
    if err != nil {
        http.Error(w, fmt.Sprintf("Failed to exchange token: %v", err), http.StatusInternalServerError)
        return
    }

    // Store token securely
    if err := StoreToken("github", token); err != nil {
        http.Error(w, fmt.Sprintf("Failed to store token: %v", err), http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    if err := json.NewEncoder(w).Encode(map[string]string{
        "status": "success",
        "message": "GitHub OAuth token stored successfully",
    }); err != nil {
        http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
    }
}

// InstallationConfig represents GitHub App installation configuration
type InstallationConfig struct {
    AppID          int64
    PrivateKey     []byte
    WebhookSecret  string
    InstallationID int64
}

// AppAuthHandler handles GitHub App authentication
type AppAuthHandler struct {
    config InstallationConfig
}

// NewAppAuthHandler creates a new GitHub App auth handler
func NewAppAuthHandler(config InstallationConfig) *AppAuthHandler {
    return &AppAuthHandler{
        config: config,
    }
}

// GetInstallationToken generates an installation access token
// Note: This requires GitHub App JWT authentication (implementation simplified)
func (h *AppAuthHandler) GetInstallationToken(ctx context.Context) (string, error) {
    // In production, implement proper GitHub App JWT token generation
    // using the crypto/rsa package and the App's private key
    //
    // Steps:
    // 1. Generate JWT with App ID and current timestamp
    // 2. Sign with private key
    // 3. Use JWT to request installation access token
    // 4. Return installation access token

    return "", errors.NewError(
        errors.ErrorCodeValidationFailed,
        "GitHub App authentication not fully implemented",
        errors.SeverityError,
        errors.CategoryValidation,
    ).WithSuggestion("Use OAuth token authentication instead")
}
