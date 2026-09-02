// Package auth provides shared authentication compatibility layer generation
// for validation and testing. This package centralizes auth SQL generation
// to eliminate duplication across plugins and validation systems.
package auth

import (
	_ "github.com/capysquash/pgsquash-engine/internal/utils" // Preload for future use

	"github.com/capysquash/pgsquash-engine/internal/errors"
)

// ServiceType represents different authentication service providers
type ServiceType string

const (
	ServiceClerk    ServiceType = "clerk"
	ServiceSupabase ServiceType = "supabase"
	ServiceAuth0    ServiceType = "auth0"
	ServiceNextAuth ServiceType = "nextauth"
	ServiceFirebase ServiceType = "firebase"
)

// CompatibilityGenerator generates authentication compatibility SQL
// for validation environments. Creates mock schemas, roles, and functions
// that allow migrations to be validated without actual auth services.
type CompatibilityGenerator struct {
	service ServiceType
}

// NewCompatibilityGenerator creates a new auth compatibility generator
func NewCompatibilityGenerator(service ServiceType) *CompatibilityGenerator {
	return &CompatibilityGenerator{
		service: service,
	}
}

// Generate returns the complete compatibility SQL for the configured service
func (g *CompatibilityGenerator) Generate() string {
	switch g.service {
	case ServiceClerk:
		return g.GenerateClerkCompatibility()
	case ServiceSupabase:
		return g.GenerateSupabaseCompatibility()
	case ServiceAuth0:
		return g.GenerateAuth0Compatibility()
	case ServiceNextAuth:
		return g.GenerateNextAuthCompatibility()
	case ServiceFirebase:
		return g.GenerateFirebaseCompatibility()
	default:
		return ""
	}
}

// GenerateClerkCompatibility creates Clerk authentication compatibility layer
// This includes:
//   - auth schema
//   - Common roles (anon, authenticated, service_role)
//   - Mock auth.jwt() function with Clerk JWT v2 organization structure
//   - Helper functions (current_user_id, current_organization_id, etc.)
func (g *CompatibilityGenerator) GenerateClerkCompatibility() string {
	return `-- Clerk Authentication Compatibility Layer
CREATE SCHEMA IF NOT EXISTS auth;

-- Create common Supabase/Clerk roles (used in RLS policies)
DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'anon') THEN
    CREATE ROLE anon NOLOGIN;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'authenticated') THEN
    CREATE ROLE authenticated NOLOGIN;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'service_role') THEN
    CREATE ROLE service_role NOLOGIN;
  END IF;
END
$$;

-- Mock Clerk auth.jwt() function
-- NOTE: No volatility marker - let PostgreSQL use defaults for test mocks
CREATE OR REPLACE FUNCTION auth.jwt() RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER AS $$
BEGIN
  -- Return a mock Clerk JWT payload for validation purposes
  RETURN jsonb_build_object(
    'o', jsonb_build_object(
      'id', 'org_mock_organization_id',
      'role', 'admin',
      'name', 'Mock Organization',
      'slug', 'mock-org'
    ),
    'sub', 'user_mock_user_id',
    'email', 'mock@clerk.dev',
    'email_verified', true,
    'iat', extract(epoch from now()),
    'exp', extract(epoch from now()) + 3600,
    'iss', 'https://mock-clerk.clerk.accounts.dev'
  );
END;
$$;

-- Clerk-compatible claim helper used by many migrations
-- NOTE: No volatility marker - let PostgreSQL use defaults for test mocks
CREATE OR REPLACE FUNCTION current_clerk_claims() RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER AS $$
BEGIN
  RETURN auth.jwt();
END;
$$;

-- Clerk user-id helper used by policies and RPCs
-- NOTE: No volatility marker - let PostgreSQL use defaults for test mocks
CREATE OR REPLACE FUNCTION clerk_user_id() RETURNS text LANGUAGE plpgsql SECURITY DEFINER AS $$
BEGIN
  RETURN COALESCE(
    current_clerk_claims()->>'sub',
    current_clerk_claims()->>'user_id',
    current_setting('app.clerk_user_id', true)
  );
END;
$$;

-- Backward-compatible alias used in some migration sets
-- NOTE: No volatility marker - let PostgreSQL use defaults for test mocks
CREATE OR REPLACE FUNCTION current_clerk_user_id() RETURNS text LANGUAGE plpgsql SECURITY DEFINER AS $$
BEGIN
  RETURN clerk_user_id();
END;
$$;

-- Clerk admin helper used in RLS predicates
-- NOTE: No volatility marker - let PostgreSQL use defaults for test mocks
CREATE OR REPLACE FUNCTION clerk_is_admin() RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER AS $$
DECLARE
  app_role text;
  org_role text;
BEGIN
  app_role := lower(COALESCE(
    current_clerk_claims()->>'app_role',
    current_clerk_claims()->'publicMetadata'->>'role',
    current_clerk_claims()->'privateMetadata'->>'role',
    current_clerk_claims()->'app_metadata'->>'role',
    current_clerk_claims()->'user_metadata'->>'role',
    current_clerk_claims()->>'role'
  ));

  org_role := lower(COALESCE(
    current_clerk_claims()->'org'->>'role',
    current_clerk_claims()->'o'->>'role'
  ));

  RETURN app_role IN ('admin', 'super_admin', 'owner')
    OR org_role IN ('admin', 'owner');
END;
$$;

-- Mock current_user_id helper (common in Clerk setups)
-- NOTE: No volatility marker - let PostgreSQL use defaults for test mocks
CREATE OR REPLACE FUNCTION current_user_id() RETURNS text LANGUAGE plpgsql SECURITY DEFINER AS $$
BEGIN
  RETURN clerk_user_id();
END;
$$;

-- Mock organization helpers
-- NOTE: No volatility markers - let PostgreSQL use defaults for test mocks
CREATE OR REPLACE FUNCTION current_organization_id() RETURNS text LANGUAGE plpgsql SECURITY DEFINER AS $$
BEGIN
  RETURN COALESCE(
    (auth.jwt()->'org'->>'id')::text,
    (auth.jwt()->'o'->>'id')::text
  );
END;
$$;

CREATE OR REPLACE FUNCTION current_organization_role() RETURNS text LANGUAGE plpgsql SECURITY DEFINER AS $$
BEGIN
  RETURN COALESCE(
    (auth.jwt()->'org'->>'role')::text,
    (auth.jwt()->'o'->>'role')::text
  );
END;
$$;

CREATE OR REPLACE FUNCTION current_organization_name() RETURNS text LANGUAGE plpgsql SECURITY DEFINER AS $$
BEGIN
  RETURN COALESCE(
    (auth.jwt()->'org'->>'name')::text,
    (auth.jwt()->'o'->>'name')::text
  );
END;
$$;

-- Clerk-style org helper aliases frequently used in production migrations
-- NOTE: No volatility markers - let PostgreSQL use defaults for test mocks
CREATE OR REPLACE FUNCTION current_clerk_org_id() RETURNS text LANGUAGE plpgsql SECURITY DEFINER AS $$
BEGIN
  RETURN current_organization_id();
END;
$$;

CREATE OR REPLACE FUNCTION current_clerk_org_role() RETURNS text LANGUAGE plpgsql SECURITY DEFINER AS $$
BEGIN
  RETURN current_organization_role();
END;
$$;

CREATE OR REPLACE FUNCTION current_clerk_org_name() RETURNS text LANGUAGE plpgsql SECURITY DEFINER AS $$
BEGIN
  RETURN current_organization_name();
END;
$$;

-- Common identity helpers
-- NOTE: No volatility markers - let PostgreSQL use defaults for test mocks
CREATE OR REPLACE FUNCTION clerk_user_email() RETURNS text LANGUAGE plpgsql SECURITY DEFINER AS $$
BEGIN
  RETURN NULLIF(current_clerk_claims()->>'email', '');
END;
$$;

CREATE OR REPLACE FUNCTION is_authenticated() RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER AS $$
BEGIN
  RETURN clerk_user_id() IS NOT NULL;
END;
$$;

CREATE OR REPLACE FUNCTION user_has_valid_mfa() RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER AS $$
DECLARE
  mfa_age integer;
BEGIN
  BEGIN
    mfa_age := (current_clerk_claims()->'fva'->>1)::integer;
  EXCEPTION WHEN others THEN
    RETURN FALSE;
  END;

  RETURN COALESCE(mfa_age != -1, FALSE);
END;
$$;

CREATE OR REPLACE FUNCTION get_clerk_user_id() RETURNS text LANGUAGE plpgsql SECURITY DEFINER AS $$
BEGIN
  RETURN clerk_user_id();
END;
$$;

CREATE OR REPLACE FUNCTION is_clerk_admin() RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER AS $$
BEGIN
  RETURN clerk_is_admin();
END;
$$;

-- Create Supabase Realtime publication if it doesn't exist (often used with Clerk)
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_publication WHERE pubname = 'supabase_realtime') THEN
    CREATE PUBLICATION supabase_realtime;
  END IF;
END
$$;`
}

// GenerateSupabaseCompatibility creates Supabase authentication compatibility layer
// This includes:
//   - auth schema
//   - auth.users table stub for foreign key references
//   - Supabase roles (anon, authenticated, service_role)
//   - Mock auth.uid() and auth.jwt() functions
//   - Supabase Realtime publication
func (g *CompatibilityGenerator) GenerateSupabaseCompatibility() string {
	return `-- Supabase Authentication Compatibility Layer
CREATE SCHEMA IF NOT EXISTS auth;

-- Ensure extensions are available for UUID generation
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Create auth.users table stub for foreign key references
-- This allows migrations to reference auth.users(id) without errors
CREATE TABLE IF NOT EXISTS auth.users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT,
    encrypted_password TEXT,
    email_confirmed_at TIMESTAMPTZ,
    invited_at TIMESTAMPTZ,
    confirmation_token TEXT,
    confirmation_sent_at TIMESTAMPTZ,
    recovery_token TEXT,
    recovery_sent_at TIMESTAMPTZ,
    email_change_token_new TEXT,
    email_change TEXT,
    email_change_sent_at TIMESTAMPTZ,
    last_sign_in_at TIMESTAMPTZ,
    raw_app_meta_data JSONB,
    raw_user_meta_data JSONB,
    is_super_admin BOOLEAN,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    phone TEXT,
    phone_confirmed_at TIMESTAMPTZ,
    phone_change TEXT,
    phone_change_token TEXT,
    phone_change_sent_at TIMESTAMPTZ,
    confirmed_at TIMESTAMPTZ,
    email_change_token_current TEXT,
    email_change_confirm_status SMALLINT,
    banned_until TIMESTAMPTZ,
    reauthentication_token TEXT,
    reauthentication_sent_at TIMESTAMPTZ,
    is_sso_user BOOLEAN DEFAULT FALSE,
    deleted_at TIMESTAMPTZ
);

-- Create auth.identities table stub (commonly referenced in triggers)
CREATE TABLE IF NOT EXISTS auth.identities (
    id TEXT NOT NULL,
    user_id UUID NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    identity_data JSONB NOT NULL,
    provider TEXT NOT NULL,
    last_sign_in_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ,
    email TEXT,
    CONSTRAINT identities_pkey PRIMARY KEY (provider, id)
);

-- Create Supabase roles (used in RLS policies)
DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'anon') THEN
    CREATE ROLE anon NOLOGIN;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'authenticated') THEN
    CREATE ROLE authenticated NOLOGIN;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'service_role') THEN
    CREATE ROLE service_role NOLOGIN;
  END IF;
END
$$;

-- Mock Supabase auth.uid() function
-- NOTE: No volatility marker - let PostgreSQL use defaults for test mocks
CREATE OR REPLACE FUNCTION auth.uid() RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER AS $$
BEGIN
  RETURN 'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11'::uuid;
END;
$$;

-- Mock Supabase auth.jwt() function
-- NOTE: No volatility marker - let PostgreSQL use defaults for test mocks
CREATE OR REPLACE FUNCTION auth.jwt() RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER AS $$
BEGIN
  RETURN jsonb_build_object(
    'sub', 'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11',
    'email', 'mock@supabase.io',
    'role', 'authenticated',
    'iat', extract(epoch from now()),
    'exp', extract(epoch from now()) + 3600
  );
END;
$$;

-- Mock Supabase auth.email() function (commonly used in RLS policies)
-- NOTE: No volatility marker - let PostgreSQL use defaults for test mocks
CREATE OR REPLACE FUNCTION auth.email() RETURNS text LANGUAGE plpgsql SECURITY DEFINER AS $$
BEGIN
  RETURN (auth.jwt() ->> 'email')::text;
END;
$$;

-- Mock Supabase auth.role() function (commonly used in RLS policies)
-- NOTE: No volatility marker - let PostgreSQL use defaults for test mocks
CREATE OR REPLACE FUNCTION auth.role() RETURNS text LANGUAGE plpgsql SECURITY DEFINER AS $$
BEGIN
  RETURN (auth.jwt() ->> 'role')::text;
END;
$$;

-- Create Supabase Realtime publication if it doesn't exist
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_publication WHERE pubname = 'supabase_realtime') THEN
    CREATE PUBLICATION supabase_realtime;
  END IF;
END
$$;

-- Create storage schema and buckets table stub (Supabase Storage)
CREATE SCHEMA IF NOT EXISTS storage;

CREATE TABLE IF NOT EXISTS storage.buckets (
    id TEXT PRIMARY KEY,
    name TEXT UNIQUE NOT NULL,
    owner UUID REFERENCES auth.users(id),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    public BOOLEAN DEFAULT FALSE,
    avif_autodetection BOOLEAN DEFAULT FALSE,
    file_size_limit BIGINT,
    allowed_mime_types TEXT[]
);

CREATE TABLE IF NOT EXISTS storage.objects (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bucket_id TEXT REFERENCES storage.buckets(id),
    name TEXT NOT NULL,
    owner UUID REFERENCES auth.users(id),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    last_accessed_at TIMESTAMPTZ,
    metadata JSONB,
    UNIQUE(bucket_id, name)
);

-- Mock Supabase storage.foldername() function (extracts folder path components)
-- NOTE: No volatility marker - let PostgreSQL use defaults for test mocks
CREATE OR REPLACE FUNCTION storage.foldername(name TEXT) RETURNS TEXT[] LANGUAGE plpgsql AS $$
BEGIN
  -- Split path by '/' and return array of folder components
  -- Example: 'user_id/avatars/image.png' returns ['user_id', 'avatars', 'image.png']
  RETURN string_to_array(name, '/');
END;
$$;`
}

// GenerateAuth0Compatibility creates Auth0 authentication compatibility layer
func (g *CompatibilityGenerator) GenerateAuth0Compatibility() string {
	return `-- Auth0 Authentication Compatibility Layer
CREATE SCHEMA IF NOT EXISTS auth;

-- Mock Auth0 JWT function
-- NOTE: No volatility marker - let PostgreSQL use defaults for test mocks
CREATE OR REPLACE FUNCTION auth.jwt() RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER AS $$
BEGIN
  RETURN jsonb_build_object(
    'sub', 'auth0|mock_user_id',
    'email', 'mock@auth0.com',
    'email_verified', true,
    'iss', 'https://mock.auth0.com/',
    'aud', 'mock_audience',
    'iat', extract(epoch from now()),
    'exp', extract(epoch from now()) + 3600
  );
END;
$$;`
}

// GenerateNextAuthCompatibility creates NextAuth authentication compatibility layer
func (g *CompatibilityGenerator) GenerateNextAuthCompatibility() string {
	return `-- NextAuth.js Authentication Compatibility Layer
CREATE SCHEMA IF NOT EXISTS public;

-- NextAuth required tables
CREATE TABLE IF NOT EXISTS accounts (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    type TEXT NOT NULL,
    provider TEXT NOT NULL,
    provider_account_id TEXT NOT NULL,
    refresh_token TEXT,
    access_token TEXT,
    expires_at INTEGER,
    token_type TEXT,
    scope TEXT,
    id_token TEXT,
    session_state TEXT,
    UNIQUE(provider, provider_account_id)
);

CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    session_token TEXT NOT NULL UNIQUE,
    user_id TEXT NOT NULL,
    expires TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    name TEXT,
    email TEXT UNIQUE,
    email_verified TIMESTAMPTZ,
    image TEXT
);

CREATE TABLE IF NOT EXISTS verification_tokens (
    identifier TEXT NOT NULL,
    token TEXT NOT NULL UNIQUE,
    expires TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (identifier, token)
);`
}

// GenerateFirebaseCompatibility creates Firebase authentication compatibility layer
func (g *CompatibilityGenerator) GenerateFirebaseCompatibility() string {
	return `-- Firebase Authentication Compatibility Layer
CREATE SCHEMA IF NOT EXISTS auth;

-- Mock Firebase JWT function
-- NOTE: No volatility marker - let PostgreSQL use defaults for test mocks
CREATE OR REPLACE FUNCTION auth.jwt() RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER AS $$
BEGIN
  RETURN jsonb_build_object(
    'sub', 'mock_firebase_uid',
    'email', 'mock@firebase.com',
    'email_verified', true,
    'iss', 'https://securetoken.google.com/mock-project',
    'aud', 'mock-project',
    'iat', extract(epoch from now()),
    'exp', extract(epoch from now()) + 3600,
    'firebase', jsonb_build_object(
      'identities', jsonb_build_object(
        'email', jsonb_build_array('mock@firebase.com')
      ),
      'sign_in_provider', 'password'
    )
  );
END;
$$;`
}

// GetServiceFromString converts string to ServiceType
func GetServiceFromString(s string) (ServiceType, error) {
	switch s {
	case "clerk":
		return ServiceClerk, nil
	case "supabase":
		return ServiceSupabase, nil
	case "auth0":
		return ServiceAuth0, nil
	case "nextauth":
		return ServiceNextAuth, nil
	case "firebase":
		return ServiceFirebase, nil
	default:
		return "", errors.New(
			errors.ErrorCodeValidationFailed,
			errors.CategoryValidation,
			"unknown auth service",
			map[string]any{"service": s},
		)
	}
}
