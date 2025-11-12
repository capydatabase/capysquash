#!/bin/bash
set -euo pipefail

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[SAMPLE]${NC} $1" >&2
}

log_success() {
    echo -e "${GREEN}[SAMPLE]${NC} $1" >&2
}

# Create sample migrations directory
MIGRATIONS_DIR="${1:-/app/migrations}"

log_info "Creating sample migration files in: $MIGRATIONS_DIR"

# Ensure directory exists
mkdir -p "$MIGRATIONS_DIR"

# Sample migration 1: Initial schema
cat > "$MIGRATIONS_DIR/001_initial_schema.sql" << 'EOF'
-- Initial database schema
-- This migration sets up the basic user management tables

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Users table
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    email VARCHAR(255) NOT NULL,
    username VARCHAR(50) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Create basic indexes
CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_username ON users(username);
EOF

# Sample migration 2: Add user profiles
cat > "$MIGRATIONS_DIR/002_add_user_profiles.sql" << 'EOF'
-- Add user profile information
-- This migration extends users with profile data

-- Add profile columns to users
ALTER TABLE users ADD COLUMN first_name VARCHAR(100);
ALTER TABLE users ADD COLUMN last_name VARCHAR(100);
ALTER TABLE users ADD COLUMN bio TEXT;
ALTER TABLE users ADD COLUMN avatar_url VARCHAR(500);

-- Add profile completion tracking
ALTER TABLE users ADD COLUMN profile_completed BOOLEAN DEFAULT FALSE;
EOF

# Sample migration 3: Create posts table
cat > "$MIGRATIONS_DIR/003_create_posts.sql" << 'EOF'
-- Posts and content management
-- This migration adds content creation capabilities

CREATE TABLE posts (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title VARCHAR(255) NOT NULL,
    content TEXT NOT NULL,
    published BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Indexes for posts
CREATE INDEX idx_posts_user_id ON posts(user_id);
CREATE INDEX idx_posts_published ON posts(published);
CREATE INDEX idx_posts_created_at ON posts(created_at);
EOF

# Sample migration 4: Add constraints and modify posts
cat > "$MIGRATIONS_DIR/004_enhance_posts.sql" << 'EOF'
-- Enhance posts with additional features
-- This migration adds tags, categories, and constraints

-- Add unique constraint to users email
ALTER TABLE users ADD CONSTRAINT users_email_unique UNIQUE(email);
ALTER TABLE users ADD CONSTRAINT users_username_unique UNIQUE(username);

-- Add category to posts
ALTER TABLE posts ADD COLUMN category VARCHAR(50) DEFAULT 'general';

-- Add slug for SEO-friendly URLs
ALTER TABLE posts ADD COLUMN slug VARCHAR(255);

-- Create unique index on slug
CREATE UNIQUE INDEX idx_posts_slug ON posts(slug);

-- Add check constraint for category
ALTER TABLE posts ADD CONSTRAINT posts_category_check
    CHECK (category IN ('general', 'tech', 'lifestyle', 'business', 'personal'));
EOF

# Sample migration 5: Comments system
cat > "$MIGRATIONS_DIR/005_add_comments.sql" << 'EOF'
-- Comments system
-- This migration adds commenting capabilities

CREATE TABLE comments (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    post_id UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    parent_id UUID REFERENCES comments(id) ON DELETE CASCADE,
    content TEXT NOT NULL,
    approved BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Indexes for comments
CREATE INDEX idx_comments_post_id ON comments(post_id);
CREATE INDEX idx_comments_user_id ON comments(user_id);
CREATE INDEX idx_comments_parent_id ON comments(parent_id);
CREATE INDEX idx_comments_created_at ON comments(created_at);
EOF

# Sample migration 6: Tags system (many-to-many)
cat > "$MIGRATIONS_DIR/006_add_tags_system.sql" << 'EOF'
-- Tags system with many-to-many relationships
-- This migration adds flexible tagging

-- Tags table
CREATE TABLE tags (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(50) NOT NULL UNIQUE,
    color VARCHAR(7) DEFAULT '#000000', -- Hex color
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Junction table for posts and tags
CREATE TABLE post_tags (
    post_id UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    tag_id UUID NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (post_id, tag_id)
);

-- Indexes for the junction table
CREATE INDEX idx_post_tags_post_id ON post_tags(post_id);
CREATE INDEX idx_post_tags_tag_id ON post_tags(tag_id);

-- Index on tag name for searching
CREATE INDEX idx_tags_name ON tags(name);
EOF

# Sample migration 7: User preferences and settings
cat > "$MIGRATIONS_DIR/007_user_preferences.sql" << 'EOF'
-- User preferences and settings
-- This migration adds user customization options

CREATE TABLE user_preferences (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    theme VARCHAR(20) DEFAULT 'light',
    language VARCHAR(5) DEFAULT 'en',
    timezone VARCHAR(50) DEFAULT 'UTC',
    email_notifications BOOLEAN DEFAULT TRUE,
    push_notifications BOOLEAN DEFAULT TRUE,
    privacy_level VARCHAR(20) DEFAULT 'public',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Unique constraint - one preference record per user
ALTER TABLE user_preferences ADD CONSTRAINT user_preferences_user_id_unique UNIQUE(user_id);

-- Check constraints for valid values
ALTER TABLE user_preferences ADD CONSTRAINT user_preferences_theme_check
    CHECK (theme IN ('light', 'dark', 'auto'));

ALTER TABLE user_preferences ADD CONSTRAINT user_preferences_privacy_check
    CHECK (privacy_level IN ('public', 'friends', 'private'));
EOF

# Sample migration 8: Performance optimization
cat > "$MIGRATIONS_DIR/008_performance_optimization.sql" << 'EOF'
-- Performance optimization
-- This migration adds indexes and optimizations for better performance

-- Add partial indexes for active users
CREATE INDEX idx_users_active_email ON users(email) WHERE updated_at > NOW() - INTERVAL '30 days';

-- Add composite index for post queries
CREATE INDEX idx_posts_user_published_created ON posts(user_id, published, created_at DESC);

-- Add GIN index for full-text search on posts
CREATE INDEX idx_posts_content_search ON posts USING GIN(to_tsvector('english', title || ' ' || content));

-- Add index for comment threads
CREATE INDEX idx_comments_post_parent_created ON comments(post_id, parent_id, created_at);

-- Update statistics for better query planning
ANALYZE users;
ANALYZE posts;
ANALYZE comments;
ANALYZE tags;
EOF

# Sample migration 9: Functions and triggers
cat > "$MIGRATIONS_DIR/009_add_functions_triggers.sql" << 'EOF'
-- Functions and triggers for automation
-- This migration adds automated features

-- Function to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Triggers for automatic timestamp updates
CREATE TRIGGER users_updated_at_trigger
    BEFORE UPDATE ON users
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER posts_updated_at_trigger
    BEFORE UPDATE ON posts
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER comments_updated_at_trigger
    BEFORE UPDATE ON comments
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at();

-- Function to generate slug from title
CREATE OR REPLACE FUNCTION generate_slug(title_text TEXT)
RETURNS TEXT AS $$
BEGIN
    RETURN LOWER(
        REGEXP_REPLACE(
            REGEXP_REPLACE(title_text, '[^a-zA-Z0-9\s-]', '', 'g'),
            '\s+', '-', 'g'
        )
    );
END;
$$ LANGUAGE plpgsql IMMUTABLE;

-- Trigger to auto-generate slug for posts
CREATE OR REPLACE FUNCTION auto_generate_post_slug()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.slug IS NULL OR NEW.slug = '' THEN
        NEW.slug = generate_slug(NEW.title);

        -- Ensure uniqueness
        WHILE EXISTS (SELECT 1 FROM posts WHERE slug = NEW.slug AND id != NEW.id) LOOP
            NEW.slug = NEW.slug || '-' || EXTRACT(EPOCH FROM NOW())::INTEGER;
        END LOOP;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER posts_auto_slug_trigger
    BEFORE INSERT OR UPDATE ON posts
    FOR EACH ROW
    EXECUTE FUNCTION auto_generate_post_slug();
EOF

# Sample migration 10: Row Level Security (RLS)
cat > "$MIGRATIONS_DIR/010_add_row_level_security.sql" << 'EOF'
-- Row Level Security (RLS) implementation
-- This migration adds security policies

-- Enable RLS on sensitive tables
ALTER TABLE users ENABLE ROW LEVEL SECURITY;
ALTER TABLE posts ENABLE ROW LEVEL SECURITY;
ALTER TABLE comments ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_preferences ENABLE ROW LEVEL SECURITY;

-- Users can only see their own data
CREATE POLICY users_self_policy ON users
    FOR ALL
    TO authenticated
    USING (auth.uid() = id);

-- Posts policies
CREATE POLICY posts_select_policy ON posts
    FOR SELECT
    TO authenticated
    USING (published = TRUE OR user_id = auth.uid());

CREATE POLICY posts_insert_policy ON posts
    FOR INSERT
    TO authenticated
    WITH CHECK (user_id = auth.uid());

CREATE POLICY posts_update_policy ON posts
    FOR UPDATE
    TO authenticated
    USING (user_id = auth.uid())
    WITH CHECK (user_id = auth.uid());

CREATE POLICY posts_delete_policy ON posts
    FOR DELETE
    TO authenticated
    USING (user_id = auth.uid());

-- Comments policies
CREATE POLICY comments_select_policy ON comments
    FOR SELECT
    TO authenticated
    USING (approved = TRUE OR user_id = auth.uid());

CREATE POLICY comments_insert_policy ON comments
    FOR INSERT
    TO authenticated
    WITH CHECK (user_id = auth.uid());

CREATE POLICY comments_update_policy ON comments
    FOR UPDATE
    TO authenticated
    USING (user_id = auth.uid())
    WITH CHECK (user_id = auth.uid());

-- User preferences - only own data
CREATE POLICY user_preferences_policy ON user_preferences
    FOR ALL
    TO authenticated
    USING (user_id = auth.uid())
    WITH CHECK (user_id = auth.uid());
EOF

# Sample migration 11: Cleanup and optimization (creates cycles for testing)
cat > "$MIGRATIONS_DIR/011_cleanup_optimization.sql" << 'EOF'
-- Cleanup and optimization
-- This migration creates some cycles and patterns for testing squashing

-- Drop and recreate some indexes with better names
DROP INDEX IF EXISTS idx_users_email;
CREATE INDEX idx_users_email_lookup ON users(email);

-- Modify a column and change it back (creates a cycle)
ALTER TABLE users ALTER COLUMN email TYPE TEXT;
ALTER TABLE users ALTER COLUMN email TYPE VARCHAR(255);

-- Add a temporary column and remove it (transient column)
ALTER TABLE posts ADD COLUMN temp_migration_id INTEGER;
ALTER TABLE posts DROP COLUMN temp_migration_id;

-- Create a function and then recreate it (for deduplication testing)
CREATE OR REPLACE FUNCTION get_user_post_count(user_uuid UUID)
RETURNS INTEGER AS $$
BEGIN
    RETURN (SELECT COUNT(*) FROM posts WHERE user_id = user_uuid);
END;
$$ LANGUAGE plpgsql;

-- Drop and recreate the same function (duplicate)
DROP FUNCTION IF EXISTS get_user_post_count(UUID);
CREATE OR REPLACE FUNCTION get_user_post_count(user_uuid UUID)
RETURNS INTEGER AS $$
BEGIN
    RETURN (SELECT COUNT(*) FROM posts WHERE user_id = user_uuid AND published = TRUE);
END;
$$ LANGUAGE plpgsql;

-- Add some constraints and remove others
ALTER TABLE posts ADD CONSTRAINT posts_title_not_empty CHECK (LENGTH(title) > 0);
ALTER TABLE posts DROP CONSTRAINT IF EXISTS posts_category_check;
ALTER TABLE posts ADD CONSTRAINT posts_category_valid
    CHECK (category IN ('general', 'tech', 'lifestyle', 'business', 'personal', 'news'));

-- Final optimization
VACUUM ANALYZE;
EOF

log_success "☑ Created 11 sample migration files"
log_info "Sample migrations include:"
log_info "  ► Basic schema creation"
log_info "  ► Column additions and modifications"
log_info "  ► Index creation and optimization"
log_info "  ► Constraints and relationships"
log_info "  ► Functions and triggers"
log_info "  ► Row Level Security (RLS)"
log_info "  ► Cycles and patterns for testing squashing"
echo
log_info "These migrations are designed to test various pgsquash features:"
log_info "  🔄 CREATE-ALTER consolidation"
log_info "  🗑️  DROP-CREATE cycles"
log_info "  ⚡ Transient objects"
log_info "  🔧 Function deduplication"
log_info "  🛡️  RLS consolidation"
log_info "  📊 Complex object relationships"