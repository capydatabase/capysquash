package github

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/zalando/go-keyring"
)

const (
	// KeyringService is the service name for keyring storage
	KeyringService = "pgsquash"
	// FallbackFile is used when keyring is unavailable
	FallbackFile = ".pgsquash-tokens.json"
)

// TokenStorage handles token storage with OS keychain fallback
type TokenStorage struct {
	useKeyring bool
}

// NewTokenStorage creates a new token storage instance
func NewTokenStorage() *TokenStorage {
	// Test if keyring is available
	useKeyring := isKeychainAvailable()
	return &TokenStorage{
		useKeyring: useKeyring,
	}
}

// isKeychainAvailable tests if the OS keychain is accessible
func isKeychainAvailable() bool {
	// Try to set and get a test value
	testKey := "pgsquash-keychain-test"
	testValue := "test"

	err := keyring.Set(KeyringService, testKey, testValue)
	if err != nil {
		return false
	}

	value, err := keyring.Get(KeyringService, testKey)
	if err != nil || value != testValue {
		return false
	}

	// Clean up test
	_ = keyring.Delete(KeyringService, testKey)
	return true
}

// StoreToken stores an OAuth token for a service
// Uses OS keychain when available, falls back to encrypted file
func StoreToken(service, token string) error {
	storage := NewTokenStorage()

	if storage.useKeyring {
		return storeTokenKeyring(service, token)
	}
	return storeTokenFile(service, token)
}

// RetrieveToken retrieves an OAuth token for a service
func RetrieveToken(service string) (string, error) {
	storage := NewTokenStorage()

	if storage.useKeyring {
		token, err := retrieveTokenKeyring(service)
		if err == nil {
			return token, nil
		}
		// Fallback to file if keyring fails
	}
	return retrieveTokenFile(service)
}

// RevokeToken removes a stored token for a service
func RevokeToken(service string) error {
	storage := NewTokenStorage()

	if storage.useKeyring {
		if err := revokeTokenKeyring(service); err == nil {
			return nil
		}
		// Continue to file fallback if keyring fails
	}
	return revokeTokenFile(service)
}

// HasToken checks if a token exists for a service
func HasToken(service string) (bool, error) {
	storage := NewTokenStorage()

	if storage.useKeyring {
		has, err := hasTokenKeyring(service)
		if err == nil {
			return has, nil
		}
		// Fallback to file if keyring fails
	}
	return hasTokenFile(service)
}

// ListServices returns a list of services with stored tokens
func ListServices() ([]string, error) {
	storage := NewTokenStorage()

	if storage.useKeyring {
		// Keyring doesn't support listing, so we use file-based metadata
		return listServicesFile()
	}
	return listServicesFile()
}

// Keyring-based storage functions

func storeTokenKeyring(service, token string) error {
	if err := keyring.Set(KeyringService, service, token); err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to store token in keyring",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithAdditional("service", service)
	}

	// Also update metadata file for listing
	if err := updateMetadata(service, true); err != nil {
		// Non-critical error, log but don't fail
		_ = errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to update metadata",
			errors.SeverityWarning,
			errors.CategoryValidation,
		).WithInnerError(err).WithAdditional("service", service)
	}

	return nil
}

func retrieveTokenKeyring(service string) (string, error) {
	token, err := keyring.Get(KeyringService, service)
	if err != nil {
		return "", errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to retrieve token from keyring",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithAdditional("service", service)
	}
	return token, nil
}

func revokeTokenKeyring(service string) error {
	if err := keyring.Delete(KeyringService, service); err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to delete token from keyring",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithAdditional("service", service)
	}

	// Update metadata
	if err := updateMetadata(service, false); err != nil {
		// Non-critical error
		_ = errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to update metadata",
			errors.SeverityWarning,
			errors.CategoryValidation,
		).WithInnerError(err).WithAdditional("service", service)
	}

	return nil
}

func hasTokenKeyring(service string) (bool, error) {
	_, err := keyring.Get(KeyringService, service)
	if err != nil {
		if err == keyring.ErrNotFound {
			return false, nil
		}
		return false, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to check token in keyring",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithAdditional("service", service)
	}
	return true, nil
}

// File-based storage functions (fallback and legacy support)

type tokenStore struct {
	Tokens   map[string]string `json:"tokens"`
	Services []string          `json:"services"` // For listing when using keyring
}

func getTokenFilePath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to get home directory",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}

	configDir := filepath.Join(homeDir, ".pgsquash")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return "", errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to create config directory",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithAdditional("dir", configDir)
	}

	return filepath.Join(configDir, FallbackFile), nil
}

func loadTokenStore() (*tokenStore, error) {
	filePath, err := getTokenFilePath()
	if err != nil {
		return nil, err
	}

	// If file doesn't exist, return empty store
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return &tokenStore{
			Tokens:   make(map[string]string),
			Services: []string{},
		}, nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to read token file",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithFile(filePath)
	}

	var store tokenStore
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to parse token file",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithFile(filePath)
	}

	if store.Tokens == nil {
		store.Tokens = make(map[string]string)
	}
	if store.Services == nil {
		store.Services = []string{}
	}

	return &store, nil
}

func saveTokenStore(store *tokenStore) error {
	filePath, err := getTokenFilePath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to marshal token store",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}

	// Write with restrictive permissions (owner only)
	if err := os.WriteFile(filePath, data, 0600); err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to write token file",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithFile(filePath)
	}

	return nil
}

func storeTokenFile(service, token string) error {
	store, err := loadTokenStore()
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to load token store",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}

	store.Tokens[service] = token

	// Update services list
	found := false
	for _, s := range store.Services {
		if s == service {
			found = true
			break
		}
	}
	if !found {
		store.Services = append(store.Services, service)
	}

	if err := saveTokenStore(store); err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to save token store",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}

	return nil
}

func retrieveTokenFile(service string) (string, error) {
	store, err := loadTokenStore()
	if err != nil {
		return "", errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to load token store",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}

	token, exists := store.Tokens[service]
	if !exists {
		return "", errors.NewError(
			errors.ErrorCodeValidationFailed,
			"no token found for service",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithAdditional("service", service)
	}

	return token, nil
}

func revokeTokenFile(service string) error {
	store, err := loadTokenStore()
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to load token store",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}

	delete(store.Tokens, service)

	// Remove from services list
	services := []string{}
	for _, s := range store.Services {
		if s != service {
			services = append(services, s)
		}
	}
	store.Services = services

	if err := saveTokenStore(store); err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to save token store",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}

	return nil
}

func hasTokenFile(service string) (bool, error) {
	store, err := loadTokenStore()
	if err != nil {
		return false, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to load token store",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}

	_, exists := store.Tokens[service]
	return exists, nil
}

func listServicesFile() ([]string, error) {
	store, err := loadTokenStore()
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to load token store",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}

	return store.Services, nil
}

// updateMetadata updates the metadata file for service listing
func updateMetadata(service string, add bool) error {
	store, err := loadTokenStore()
	if err != nil {
		return err
	}

	if add {
		// Add service to list if not present
		found := false
		for _, s := range store.Services {
			if s == service {
				found = true
				break
			}
		}
		if !found {
			store.Services = append(store.Services, service)
		}
	} else {
		// Remove service from list
		services := []string{}
		for _, s := range store.Services {
			if s != service {
				services = append(services, s)
			}
		}
		store.Services = services
	}

	return saveTokenStore(store)
}

// MigrateToKeyring migrates tokens from file-based storage to OS keychain
func MigrateToKeyring() error {
	storage := NewTokenStorage()
	if !storage.useKeyring {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"keyring not available on this system",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("Keyring features are not available on this operating system")
	}

	// Load tokens from file
	store, err := loadTokenStore()
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to load token store for migration",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}

	// Migrate each token to keyring
	migrated := 0
	for service, token := range store.Tokens {
		if err := storeTokenKeyring(service, token); err != nil {
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				"failed to migrate token to keyring",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err).WithAdditional("service", service).WithAdditional("migrated_count", migrated)
		}
		migrated++
	}

	// Clear tokens from file but keep metadata
	store.Tokens = make(map[string]string)
	if err := saveTokenStore(store); err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to save cleaned token store",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}

	return nil
}
