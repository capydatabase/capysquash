CREATE SCHEMA IF NOT EXISTS auth; CREATE TABLE IF NOT EXISTS auth.users (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), email text, encrypted_password text, email_confirmed_at timestamptz, invited_at timestamptz, confirmation_token text, confirmation_sent_at timestamptz, recovery_token text, recovery_sent_at timestamptz, email_change_token_new text, email_change text, email_change_sent_at timestamptz, last_sign_in_at timestamptz, raw_app_meta_data jsonb, raw_user_meta_data jsonb, is_super_admin boolean, created_at timestamptz DEFAULT now(), updated_at timestamptz DEFAULT now(), phone text, phone_confirmed_at timestamptz, phone_change text, phone_change_token text, phone_change_sent_at timestamptz, confirmed_at timestamptz, email_change_token_current text, email_change_confirm_status smallint, banned_until timestamptz, reauthentication_token text, reauthentication_sent_at timestamptz, is_sso_user boolean DEFAULT false, deleted_at timestamptz); DO $$
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
$$; CREATE OR REPLACE FUNCTION auth.uid() RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER STABLE AS $$
BEGIN
  RETURN 'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11'::uuid;
END;
$$; CREATE OR REPLACE FUNCTION auth.jwt() RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER STABLE AS $$
BEGIN
  RETURN jsonb_build_object(
    'sub', 'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11',
    'email', 'mock@supabase.io',
    'role', 'authenticated',
    'iat', extract(epoch from now()),
    'exp', extract(epoch from now()) + 3600
  );
END;
$$; CREATE OR REPLACE FUNCTION auth.email() RETURNS text LANGUAGE plpgsql SECURITY DEFINER STABLE AS $$
BEGIN
  RETURN (auth.jwt() ->> 'email')::text;
END;
$$; CREATE OR REPLACE FUNCTION auth.role() RETURNS text LANGUAGE plpgsql SECURITY DEFINER STABLE AS $$
BEGIN
  RETURN (auth.jwt() ->> 'role')::text;
END;
$$; DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_publication WHERE pubname = 'supabase_realtime') THEN
    CREATE PUBLICATION supabase_realtime;
  END IF;
END
$$; CREATE SCHEMA IF NOT EXISTS storage; CREATE TABLE IF NOT EXISTS storage.buckets (id text PRIMARY KEY, name text UNIQUE NOT NULL, owner uuid REFERENCES auth.users (id), created_at timestamptz DEFAULT now(), updated_at timestamptz DEFAULT now(), public boolean DEFAULT false, avif_autodetection boolean DEFAULT false, file_size_limit bigint, allowed_mime_types text[]); CREATE TABLE IF NOT EXISTS storage.objects (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), bucket_id text REFERENCES storage.buckets (id), name text NOT NULL, owner uuid REFERENCES auth.users (id), created_at timestamptz DEFAULT now(), updated_at timestamptz DEFAULT now(), last_accessed_at timestamptz, metadata jsonb, UNIQUE (bucket_id, name)); CREATE OR REPLACE FUNCTION storage.foldername(name text) RETURNS text[] LANGUAGE plpgsql STABLE AS $$
BEGIN
  -- Split path by '/' and return array of folder components
  -- Example: 'user_id/avatars/image.png' returns ['user_id', 'avatars', 'image.png']
  RETURN string_to_array(name, '/');
END;
$$; CREATE EXTENSION IF NOT EXISTS pg_trgm; CREATE TABLE IF NOT EXISTS public.categories (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), name text NOT NULL, slug text NOT NULL UNIQUE, description text NOT NULL, icon text, parent_id uuid REFERENCES public.categories (id) ON DELETE SET NULL, order_index int DEFAULT 0, category_type text DEFAULT 'universal' CHECK (category_type IN ('core', 'language', 'technology', 'stack', 'task', 'assistant', 'tool', 'project', 'universal')), platform_specific boolean DEFAULT false, supported_platforms text[] DEFAULT ARRAY['claude-code', 'claude-desktop', 'cursor', 'windsurf', 'windsurf-next', 'github-copilot', 'zed', 'vscode', 'vscode-insiders', 'vscodium', 'jetbrains', 'intellij', 'webstorm', 'pycharm', 'phpstorm', 'rubymine', 'clion', 'datagrip', 'goland', 'rider', 'android-studio', 'openai', 'generic-ai'], metadata jsonb DEFAULT '{}', created_at timestamptz DEFAULT now(), updated_at timestamptz DEFAULT now()); CREATE TABLE IF NOT EXISTS vdk_analytics (id uuid DEFAULT gen_random_uuid() PRIMARY KEY, user_id uuid, project_id text, event_type text NOT NULL, event_data jsonb DEFAULT '{}', metadata jsonb DEFAULT '{}', ip_address text, user_agent text, created_at timestamp with time zone DEFAULT current_timestamp); CREATE TABLE cli_integration_events (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), cli_version text NOT NULL, integration_type text NOT NULL CHECK (integration_type IN ('claude-code', 'cursor', 'windsurf', 'github-copilot', 'vscode', 'other')), action text NOT NULL CHECK (action IN ('detected', 'configured', 'activated', 'error')), success boolean NOT NULL DEFAULT true, error_message text, integration_version text, configuration_details jsonb DEFAULT ('{}'::jsonb), user_id text, session_id text NOT NULL, "timestamp" timestamp with time zone NOT NULL DEFAULT now(), created_at timestamp with time zone DEFAULT now()); CREATE TABLE cli_error_events (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), cli_version text NOT NULL, command text NOT NULL, error_type text NOT NULL, error_message text NOT NULL, stack_trace text, platform text NOT NULL, node_version text, user_id text, session_id text NOT NULL, "timestamp" timestamp with time zone NOT NULL DEFAULT now(), context jsonb DEFAULT ('{}'::jsonb), created_at timestamp with time zone DEFAULT now()); CREATE TABLE IF NOT EXISTS public.admins (email text PRIMARY KEY, added_at timestamptz DEFAULT now()); CREATE TABLE IF NOT EXISTS cli_recommendations (id uuid DEFAULT gen_random_uuid() PRIMARY KEY, recommendation_id text NOT NULL UNIQUE, project_signature jsonb NOT NULL, blueprints_recommended int NOT NULL DEFAULT 0, max_blueprints_requested int DEFAULT 20, excluded_blueprint_ids text[] DEFAULT '{}', created_at timestamp with time zone DEFAULT current_timestamp); CREATE TABLE IF NOT EXISTS user_platform_stats (id uuid DEFAULT gen_random_uuid() PRIMARY KEY, user_id uuid NOT NULL, platform text NOT NULL, usage_count int DEFAULT 1, last_used timestamp with time zone DEFAULT current_timestamp, created_at timestamp with time zone DEFAULT current_timestamp, updated_at timestamp with time zone DEFAULT current_timestamp, UNIQUE (user_id, platform)); CREATE TABLE IF NOT EXISTS public.profiles (id uuid PRIMARY KEY REFERENCES auth.users (id) ON DELETE CASCADE, email text UNIQUE NOT NULL, name text, github_username text, avatar_url text, preferred_language text, preferred_theme text DEFAULT 'system', created_at timestamptz DEFAULT now(), updated_at timestamptz DEFAULT now()); CREATE TABLE IF NOT EXISTS vdk_versions (id uuid DEFAULT gen_random_uuid() PRIMARY KEY, component text NOT NULL, version text NOT NULL, status text NOT NULL DEFAULT 'stable', description text, security_patch boolean DEFAULT false, download_url text, created_at timestamp with time zone DEFAULT current_timestamp, updated_at timestamp with time zone DEFAULT current_timestamp); CREATE TABLE IF NOT EXISTS community_blueprints (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), slug varchar(255) UNIQUE NOT NULL, title varchar(500) NOT NULL, description text, content text NOT NULL, author_id uuid NOT NULL, author_username varchar(255) NOT NULL, category varchar(100), framework varchar(100), language varchar(100), tags jsonb DEFAULT ('[]'::jsonb), complexity varchar(50) DEFAULT 'intermediate' CHECK (complexity IN ('beginner', 'intermediate', 'advanced')), platforms jsonb DEFAULT ('{}'::jsonb), relationships jsonb DEFAULT ('{}'::jsonb), usage_count int DEFAULT 0 CHECK (usage_count >= 0), rating numeric(3, 2) DEFAULT 0 CHECK (rating >= 0 AND rating <= 5), vote_count int DEFAULT 0 CHECK (vote_count >= 0), deployment_success_rate numeric(3, 2) DEFAULT 0 CHECK (deployment_success_rate >= 0 AND deployment_success_rate <= 1), status varchar(50) DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'archived')), created_at timestamp with time zone DEFAULT now(), updated_at timestamp with time zone DEFAULT now(), published_at timestamp with time zone); CREATE TABLE IF NOT EXISTS user_command_stats (id uuid DEFAULT gen_random_uuid() PRIMARY KEY, user_id uuid NOT NULL, command text NOT NULL, usage_count int DEFAULT 1, last_used timestamp with time zone DEFAULT current_timestamp, created_at timestamp with time zone DEFAULT current_timestamp, updated_at timestamp with time zone DEFAULT current_timestamp, UNIQUE (user_id, command)); CREATE TABLE cli_usage_events (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), cli_version text NOT NULL, command text NOT NULL, platform text NOT NULL, node_version text, execution_time_ms int NOT NULL, success boolean NOT NULL DEFAULT true, error_message text, project_type text, blueprints_generated int, integrations_detected text[], user_id text, session_id text NOT NULL, "timestamp" timestamp with time zone NOT NULL DEFAULT now(), metadata jsonb DEFAULT ('{}'::jsonb), created_at timestamp with time zone DEFAULT now()); CREATE TABLE IF NOT EXISTS user_project_stats (id uuid DEFAULT gen_random_uuid() PRIMARY KEY, user_id uuid NOT NULL, project_id text NOT NULL, last_sync timestamp with time zone DEFAULT current_timestamp, sync_count int DEFAULT 1, blueprints_synced int DEFAULT 0, created_at timestamp with time zone DEFAULT current_timestamp, updated_at timestamp with time zone DEFAULT current_timestamp, UNIQUE (user_id, project_id)); CREATE TABLE IF NOT EXISTS public.wizard_configurations (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid REFERENCES auth.users (id) ON DELETE SET NULL, session_id text, stack_choices jsonb NOT NULL, language_choices jsonb NOT NULL, tool_preferences jsonb NOT NULL, environment_details jsonb NOT NULL, ai_assistant_choices jsonb DEFAULT '{}' NOT NULL, output_format text DEFAULT 'zip', custom_requirements text, generated_blueprints text[], generation_timestamp timestamptz DEFAULT now(), created_at timestamptz DEFAULT now()); CREATE TABLE IF NOT EXISTS public.generation_templates (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), name text NOT NULL UNIQUE, description text, template_content text NOT NULL, output_format text NOT NULL, is_active boolean DEFAULT true, created_at timestamptz DEFAULT now(), updated_at timestamptz DEFAULT now()); CREATE TABLE IF NOT EXISTS team_configurations (id uuid DEFAULT gen_random_uuid() PRIMARY KEY, team_id text NOT NULL, user_id uuid NOT NULL, team_name text DEFAULT 'Team', main_config jsonb DEFAULT '{}', rules_config jsonb DEFAULT '{}', settings_config jsonb DEFAULT '{}', created_at timestamp with time zone DEFAULT current_timestamp, updated_at timestamp with time zone DEFAULT current_timestamp, UNIQUE (team_id, user_id)); CREATE TABLE IF NOT EXISTS public.sync_logs (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), sync_type text NOT NULL, added_count int DEFAULT 0, updated_count int DEFAULT 0, error_count int DEFAULT 0, errors jsonb DEFAULT ('[]'::jsonb), duration_ms int, created_at timestamptz DEFAULT now()); CREATE TABLE IF NOT EXISTS vdk_error_logs (id uuid DEFAULT gen_random_uuid() PRIMARY KEY, user_id uuid, project_id text, error_type text NOT NULL, error_message text, stack_trace text, context jsonb DEFAULT '{}', created_at timestamp with time zone DEFAULT current_timestamp); CREATE TABLE IF NOT EXISTS user_api_tokens (id uuid DEFAULT gen_random_uuid() PRIMARY KEY, user_id uuid NOT NULL REFERENCES auth.users (id) ON DELETE CASCADE, token_name text NOT NULL DEFAULT 'CLI Token', token_hash text NOT NULL UNIQUE, token_prefix text NOT NULL, last_used timestamp with time zone, expires_at timestamp with time zone, is_active boolean DEFAULT true, created_at timestamp with time zone DEFAULT current_timestamp, updated_at timestamp with time zone DEFAULT current_timestamp); CREATE TABLE IF NOT EXISTS cli_deployments (id uuid DEFAULT gen_random_uuid() PRIMARY KEY, deployment_id text NOT NULL UNIQUE, project_name text NOT NULL, project_signature jsonb NOT NULL, team_name text, blueprints_count int NOT NULL DEFAULT 0, metadata jsonb DEFAULT '{}', deployed_at timestamp with time zone DEFAULT current_timestamp, created_at timestamp with time zone DEFAULT current_timestamp, updated_at timestamp with time zone DEFAULT current_timestamp); CREATE TABLE cli_performance_events (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), cli_version text NOT NULL, command text NOT NULL, operation text NOT NULL, duration_ms int NOT NULL, memory_usage_mb numeric, files_processed int, platform text NOT NULL, session_id text NOT NULL, "timestamp" timestamp with time zone NOT NULL DEFAULT now(), created_at timestamp with time zone DEFAULT now()); CREATE TABLE IF NOT EXISTS public.collections (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), name text NOT NULL, description text NOT NULL, user_id uuid REFERENCES auth.users (id) ON DELETE CASCADE, is_public boolean DEFAULT false, created_at timestamptz DEFAULT now(), updated_at timestamptz DEFAULT now()); CREATE TABLE IF NOT EXISTS public.blueprints (id text PRIMARY KEY, title text NOT NULL, slug text NOT NULL, description text NOT NULL, content text NOT NULL, path text NOT NULL UNIQUE, category_id uuid NOT NULL REFERENCES public.categories (id) ON DELETE CASCADE, version text NOT NULL DEFAULT '1.0.0', downloads int DEFAULT 0, votes int DEFAULT 0, globs text[] DEFAULT '{}', tags text[] DEFAULT '{}', examples jsonb DEFAULT ('{}'::jsonb), compatibility jsonb DEFAULT ('{"ides": [], "aiAssistants": [], "frameworks": [], "mcpServers": []}'::jsonb), always_apply boolean DEFAULT false, frontmatter jsonb DEFAULT '{}', subcategory text, framework text, language text, stack text, complexity text CHECK (complexity IN ('simple', 'medium', 'complex')), scope text CHECK (scope IN ('file', 'component', 'feature', 'project', 'system')), audience text CHECK (audience IN ('developer', 'architect', 'team-lead', 'junior', 'senior', 'any')), maturity text CHECK (maturity IN ('experimental', 'beta', 'stable', 'deprecated')), repository_url text, license text, schema_version text DEFAULT '2.1.0', content_sections jsonb DEFAULT '[]', author text DEFAULT 'community', contributors text[] DEFAULT '{}', discussion_url text, created_at timestamptz DEFAULT now(), updated_at timestamptz DEFAULT now(), last_updated timestamptz DEFAULT now()); CREATE TABLE IF NOT EXISTS blueprint_votes (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), blueprint_id uuid NOT NULL REFERENCES community_blueprints (id) ON DELETE CASCADE, user_id uuid NOT NULL, vote_type varchar(10) NOT NULL CHECK (vote_type IN ('up', 'down')), rating int CHECK (rating IS NULL OR (rating >= 1 AND rating <= 5)), comment text, created_at timestamp with time zone DEFAULT now(), updated_at timestamp with time zone DEFAULT now(), UNIQUE (blueprint_id, user_id)); CREATE TABLE IF NOT EXISTS blueprint_usage (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), blueprint_id uuid NOT NULL REFERENCES community_blueprints (id) ON DELETE CASCADE, session_id varchar(255) NOT NULL, framework varchar(100), language varchar(100), platform varchar(100), cli_version varchar(50), success boolean DEFAULT false, platforms_deployed jsonb DEFAULT ('[]'::jsonb), adaptation_count int DEFAULT 0 CHECK (adaptation_count >= 0), compatibility_score numeric(3, 2) CHECK (compatibility_score IS NULL OR (compatibility_score >= 0 AND compatibility_score <= 1)), deployed_at timestamp with time zone DEFAULT now()); CREATE TABLE IF NOT EXISTS public.generated_packages (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), configuration_id uuid REFERENCES public.wizard_configurations (id) ON DELETE SET NULL, package_type text NOT NULL, download_url text, file_size int, blueprint_count int, download_count int DEFAULT 0, expires_at timestamptz, created_at timestamptz DEFAULT now()); CREATE TABLE IF NOT EXISTS cli_deployment_blueprints (id uuid DEFAULT gen_random_uuid() PRIMARY KEY, deployment_id text NOT NULL REFERENCES cli_deployments (deployment_id) ON DELETE CASCADE, blueprint_name text NOT NULL, blueprint_content text NOT NULL, blueprint_path text NOT NULL, blueprint_size int DEFAULT 0, created_at timestamp with time zone DEFAULT current_timestamp); CREATE TABLE IF NOT EXISTS user_blueprint_usage (id uuid DEFAULT gen_random_uuid() PRIMARY KEY, user_id uuid NOT NULL, blueprint_id text NOT NULL REFERENCES blueprints (id) ON DELETE CASCADE, usage_count int DEFAULT 1, last_used timestamp with time zone DEFAULT current_timestamp, created_at timestamp with time zone DEFAULT current_timestamp, updated_at timestamp with time zone DEFAULT current_timestamp, UNIQUE (user_id, blueprint_id)); CREATE TABLE IF NOT EXISTS public.blueprint_platforms (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), blueprint_id text REFERENCES public.blueprints (id) ON DELETE CASCADE, platform_type text NOT NULL CHECK (platform_type IN ('claude-code', 'claude-desktop', 'cursor', 'windsurf', 'windsurf-next', 'github-copilot', 'zed', 'vscode', 'vscode-insiders', 'vscodium', 'jetbrains', 'intellij', 'webstorm', 'pycharm', 'phpstorm', 'rubymine', 'clion', 'datagrip', 'goland', 'rider', 'android-studio', 'openai', 'generic-ai')), platform_config jsonb NOT NULL DEFAULT '{}', is_compatible boolean DEFAULT true, priority int DEFAULT 5 CHECK (priority >= 1 AND priority <= 10), created_at timestamptz DEFAULT now(), updated_at timestamptz DEFAULT now(), UNIQUE (blueprint_id, platform_type)); CREATE TABLE IF NOT EXISTS public.user_votes (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid REFERENCES auth.users (id) ON DELETE CASCADE, blueprint_id text REFERENCES public.blueprints (id) ON DELETE CASCADE, created_at timestamptz DEFAULT now(), UNIQUE (user_id, blueprint_id)); CREATE TABLE IF NOT EXISTS public.blueprint_validations (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), blueprint_id text REFERENCES public.blueprints (id) ON DELETE CASCADE, schema_version text NOT NULL, validation_status text NOT NULL CHECK (validation_status IN ('valid', 'warning', 'error')), validation_errors jsonb DEFAULT '[]', validation_warnings jsonb DEFAULT '[]', validated_at timestamptz DEFAULT now()); CREATE TABLE IF NOT EXISTS public.blueprint_versions (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), blueprint_id text REFERENCES public.blueprints (id) ON DELETE CASCADE, version text NOT NULL, content text NOT NULL, changes text, created_at timestamptz DEFAULT now()); CREATE TABLE IF NOT EXISTS public.blueprint_relationships (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), blueprint_id text REFERENCES public.blueprints (id) ON DELETE CASCADE, related_blueprint_id text REFERENCES public.blueprints (id) ON DELETE CASCADE, relationship_type text NOT NULL CHECK (relationship_type IN ('requires', 'suggests', 'conflicts', 'supersedes', 'enhances')), condition_tags jsonb DEFAULT '[]', condition_platforms text[] DEFAULT '{}', condition_stack text, priority int DEFAULT 1 CHECK (priority >= 1 AND priority <= 10), created_at timestamptz DEFAULT now(), UNIQUE (blueprint_id, related_blueprint_id, relationship_type)); CREATE TABLE IF NOT EXISTS public.commands (id text PRIMARY KEY, name text NOT NULL, description text NOT NULL CHECK (char_length(description) >= 10 AND char_length(description) <= 200), target text NOT NULL DEFAULT 'claude-code' CHECK (target IN ('claude-code')), command_type text NOT NULL CHECK (command_type IN ('slash', 'custom-slash', 'mcp', 'workflow', 'hook')), version text CHECK (version ~ E'^\\d+\\.\\d+\\.\\d+$'), scope text DEFAULT 'project' CHECK (scope IN ('user', 'project', 'global')), permissions jsonb DEFAULT '{}', claude_code_config jsonb DEFAULT '{}', examples jsonb DEFAULT '[]', installation jsonb DEFAULT '{}', tags text[] DEFAULT '{}', category text CHECK (category IN ('development', 'testing', 'debugging', 'refactoring', 'documentation', 'git', 'analysis', 'security', 'performance')), author text, last_updated date, compatibility_notes text, blueprint_id text REFERENCES public.blueprints (id) ON DELETE SET NULL, is_active boolean DEFAULT true, created_at timestamptz DEFAULT now(), updated_at timestamptz DEFAULT now(), CONSTRAINT valid_name_length CHECK (char_length(name) >= 1 AND char_length(name) <= 50), CONSTRAINT valid_id_format CHECK (id ~ '^[a-z0-9-]+$')); CREATE TABLE IF NOT EXISTS public.platform_commands (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), blueprint_id text REFERENCES public.blueprints (id) ON DELETE CASCADE, platform_type text NOT NULL CHECK (platform_type IN ('claude-code', 'claude-desktop', 'cursor', 'windsurf', 'windsurf-next', 'github-copilot', 'zed', 'vscode', 'vscode-insiders', 'vscodium', 'jetbrains', 'intellij', 'webstorm', 'pycharm', 'phpstorm', 'rubymine', 'clion', 'datagrip', 'goland', 'rider', 'android-studio', 'openai', 'generic-ai')), command_name text NOT NULL, command_content text NOT NULL, command_metadata jsonb DEFAULT '{}', is_active boolean DEFAULT true, created_at timestamptz DEFAULT now(), updated_at timestamptz DEFAULT now(), UNIQUE (blueprint_id, platform_type, command_name)); CREATE TABLE IF NOT EXISTS public.blueprint_dependencies (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), blueprint_id text REFERENCES public.blueprints (id) ON DELETE CASCADE, depends_on_blueprint_id text REFERENCES public.blueprints (id) ON DELETE CASCADE, dependency_type text CHECK (dependency_type IN ('requires', 'conflicts', 'enhances')), condition_tags jsonb, created_at timestamptz DEFAULT now(), UNIQUE (blueprint_id, depends_on_blueprint_id, dependency_type)); CREATE TABLE IF NOT EXISTS public.blueprint_compatibility (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), blueprint_id text REFERENCES public.blueprints (id) ON DELETE CASCADE, technology text NOT NULL, version_pattern text, compatibility_type text CHECK (compatibility_type IN ('required', 'recommended', 'optional', 'incompatible')), notes text, created_at timestamptz DEFAULT now()); CREATE OR REPLACE VIEW cli_deployment_summary AS SELECT d.deployment_id, d.project_name, d.team_name, d.blueprints_count, d.deployed_at, d.project_signature, count(r.id) AS actual_blueprints_stored, COALESCE(sum(r.blueprint_size), 0) AS total_blueprints_size FROM cli_deployments d LEFT JOIN cli_deployment_blueprints r ON d.deployment_id = r.deployment_id GROUP BY d.deployment_id, d.project_name, d.team_name, d.blueprints_count, d.deployed_at, d.project_signature ORDER BY d.deployed_at DESC; CREATE OR REPLACE VIEW public.command_search_view AS SELECT c.*, b.title AS blueprint_title, b.slug AS blueprint_slug, cat.name AS blueprint_category FROM public.commands c LEFT JOIN public.blueprints b ON c.blueprint_id = b.id LEFT JOIN public.categories cat ON b.category_id = cat.id WHERE c.is_active = true; CREATE TABLE IF NOT EXISTS public.collection_items (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), collection_id uuid REFERENCES public.collections (id) ON DELETE CASCADE, blueprint_id text REFERENCES public.blueprints (id) ON DELETE CASCADE, added_at timestamptz DEFAULT now(), UNIQUE (collection_id, blueprint_id), command_id text REFERENCES commands (id) ON DELETE CASCADE, item_type text GENERATED ALWAYS AS (CASE WHEN blueprint_id IS NOT NULL THEN 'blueprint' WHEN command_id IS NOT NULL THEN 'command' ELSE NULL END) STORED, CONSTRAINT collection_items_item_type_check CHECK ((blueprint_id IS NOT NULL AND command_id IS NULL) OR (blueprint_id IS NULL AND command_id IS NOT NULL))); CREATE OR REPLACE VIEW collection_contents AS SELECT ci.id, ci.collection_id, ci.added_at, ci.item_type, ci.blueprint_id, ci.command_id, COALESCE(b.title, c.name) AS item_name, COALESCE(b.description, c.description) AS item_description, COALESCE(b.slug, c.id) AS item_slug, b.category_id AS blueprint_category_id, c.category AS command_category, c.command_type, c.scope AS command_scope FROM collection_items ci LEFT JOIN blueprints b ON ci.blueprint_id = b.id LEFT JOIN commands c ON ci.command_id = c.id; CREATE OR REPLACE FUNCTION trigger_set_updated_at() RETURNS trigger LANGUAGE plpgsql VOLATILE AS $$
BEGIN
  NEW.updated_at = NOW();
  RETURN NEW;
END;
$$; CREATE OR REPLACE FUNCTION public.get_blueprints_by_category(category_slug text) RETURNS SETOF public.blueprints LANGUAGE plpgsql VOLATILE AS $$
BEGIN
  RETURN QUERY
  SELECT r.*
  FROM public.blueprints r
  JOIN public.categories c ON r.category_id = c.id
  WHERE c.slug = category_slug
  ORDER BY r.votes DESC, r.downloads DESC;
END;
$$; CREATE OR REPLACE FUNCTION public.increment_blueprint_downloads(target_blueprint_id text) RETURNS void LANGUAGE plpgsql VOLATILE AS $$
BEGIN
  UPDATE public.blueprints
  SET downloads = downloads + 1
  WHERE id = target_blueprint_id;
END;
$$; CREATE OR REPLACE FUNCTION public.search_blueprints(search_query text, category_slug text = NULL, tags text[] = '{}') RETURNS SETOF public.blueprints LANGUAGE plpgsql VOLATILE AS $$
BEGIN
  RETURN QUERY
  SELECT r.*
  FROM public.blueprints r
  LEFT JOIN public.categories c ON r.category_id = c.id
  WHERE
    (search_query IS NULL OR
     to_tsvector('english', r.title || ' ' || r.description || ' ' || r.content) @@ plainto_tsquery('english', search_query)) AND
    (category_slug IS NULL OR c.slug = category_slug) AND
    (array_length(tags, 1) IS NULL OR r.tags && tags)
  ORDER BY
    CASE WHEN search_query IS NOT NULL
    THEN ts_rank(to_tsvector('english', r.title || ' ' || r.description || ' ' || r.content), plainto_tsquery('english', search_query))
    ELSE 0 END DESC,
    r.votes DESC,
    r.downloads DESC;
END;
$$; CREATE OR REPLACE FUNCTION update_updated_at_column() RETURNS trigger LANGUAGE plpgsql VOLATILE AS $$
BEGIN
  NEW.updated_at = NOW();
  RETURN NEW;
END;
$$; CREATE OR REPLACE FUNCTION public.get_table_stats() RETURNS TABLE (table_name text, row_count bigint, size_bytes bigint) LANGUAGE plpgsql VOLATILE AS $$
BEGIN
  RETURN QUERY
  SELECT
    schemaname || '.' || tablename as table_name,
    COALESCE(n_tup_ins - n_tup_del, 0) as row_count,
    COALESCE(pg_total_relation_size(schemaname||'.'||tablename), 0) as size_bytes
  FROM pg_stat_user_tables
  WHERE schemaname = 'public'
  ORDER BY pg_total_relation_size(schemaname||'.'||tablename) DESC NULLS LAST;
END;
$$; CREATE OR REPLACE FUNCTION public.validate_blueprint_schema(blueprint_id text, schema_version text = '2.0.0') RETURNS jsonb LANGUAGE plpgsql VOLATILE AS $$
DECLARE
  blueprint_record public.blueprints;
  validation_result jsonb;
  errors jsonb := '[]'::jsonb;
  warnings jsonb := '[]'::jsonb;
BEGIN
  SELECT * INTO blueprint_record FROM public.blueprints WHERE id = blueprint_id;
  IF NOT FOUND THEN
    RETURN jsonb_build_object(
      'status', 'error',
      'errors', jsonb_build_array('Blueprint not found')
    );
  END IF;
  IF blueprint_record.title IS NULL OR trim(blueprint_record.title) = '' THEN
    errors := errors || jsonb_build_array('Title is required');
  END IF;
  IF blueprint_record.description IS NULL OR trim(blueprint_record.description) = '' THEN
    errors := errors || jsonb_build_array('Description is required');
  END IF;
  IF blueprint_record.content IS NULL OR trim(blueprint_record.content) = '' THEN
    errors := errors || jsonb_build_array('Content is required');
  END IF;
  IF blueprint_record.complexity IS NOT NULL AND blueprint_record.complexity NOT IN ('simple', 'medium', 'complex') THEN
    errors := errors || jsonb_build_array('Invalid complexity value');
  END IF;
  IF blueprint_record.scope IS NOT NULL AND blueprint_record.scope NOT IN ('file', 'component', 'feature', 'project', 'system') THEN
    errors := errors || jsonb_build_array('Invalid scope value');
  END IF;
  IF NOT EXISTS (SELECT 1 FROM public.blueprint_platforms WHERE blueprint_id = blueprint_record.id) THEN
    warnings := warnings || jsonb_build_array('No platform compatibility defined');
  END IF;
  validation_result := jsonb_build_object(
    'status', CASE
      WHEN jsonb_array_length(errors) > 0 THEN 'error'
      WHEN jsonb_array_length(warnings) > 0 THEN 'warning'
      ELSE 'valid'
    END,
    'errors', errors,
    'warnings', warnings,
    'validated_at', NOW()
  );
  INSERT INTO public.blueprint_validations (blueprint_id, schema_version, validation_status, validation_errors, validation_warnings)
  VALUES (blueprint_id, schema_version, (validation_result->>'status'), errors, warnings)
  ON CONFLICT (blueprint_id, schema_version)
  DO UPDATE SET
    validation_status = EXCLUDED.validation_status,
    validation_errors = EXCLUDED.validation_errors,
    validation_warnings = EXCLUDED.validation_warnings,
    validated_at = NOW();
  RETURN validation_result;
END;
$$; CREATE OR REPLACE FUNCTION public.vote_for_blueprint(target_blueprint_id text) RETURNS void LANGUAGE plpgsql VOLATILE AS $$
BEGIN
  INSERT INTO public.user_votes (user_id, blueprint_id)
  VALUES (auth.uid(), target_blueprint_id)
  ON CONFLICT (user_id, blueprint_id) DO NOTHING;
  UPDATE public.blueprints
  SET votes = (SELECT COUNT(*) FROM public.user_votes WHERE user_votes.blueprint_id = target_blueprint_id)
  WHERE id = target_blueprint_id;
END;
$$; CREATE OR REPLACE FUNCTION upsert_user_platform_stats(p_user_id uuid, p_platform text) RETURNS void LANGUAGE plpgsql VOLATILE AS $$
BEGIN
    INSERT INTO user_platform_stats (user_id, platform, usage_count, last_used)
    VALUES (p_user_id, p_platform, 1, CURRENT_TIMESTAMP)
    ON CONFLICT (user_id, platform)
    DO UPDATE SET
        usage_count = user_platform_stats.usage_count + 1,
        last_used = CURRENT_TIMESTAMP,
        updated_at = CURRENT_TIMESTAMP;
END;
$$; CREATE OR REPLACE FUNCTION get_cli_usage_summary(start_date timestamp with time zone = now() - '30 days'::interval, end_date timestamp with time zone = now()) RETURNS TABLE (total_events bigint, success_rate numeric, avg_execution_time_ms numeric, unique_sessions bigint, top_commands jsonb, platform_distribution jsonb) LANGUAGE plpgsql SECURITY DEFINER AS $$
BEGIN
  RETURN QUERY
  SELECT
    COUNT(*) as total_events,
    (COUNT(*) FILTER (WHERE success = true))::NUMERIC / NULLIF(COUNT(*), 0) * 100 as success_rate,
    AVG(execution_time_ms) as avg_execution_time_ms,
    COUNT(DISTINCT session_id) as unique_sessions,
    (
      SELECT jsonb_agg(
        jsonb_build_object('command', command, 'count', count)
      )
      FROM (
        SELECT command, COUNT(*) as count
        FROM cli_usage_events
        WHERE timestamp BETWEEN start_date AND end_date
        GROUP BY command
        ORDER BY count DESC
        LIMIT 10
      ) top_cmds
    ) as top_commands,
    (
      SELECT jsonb_agg(
        jsonb_build_object('platform', platform, 'count', count)
      )
      FROM (
        SELECT platform, COUNT(*) as count
        FROM cli_usage_events
        WHERE timestamp BETWEEN start_date AND end_date
        GROUP BY platform
        ORDER BY count DESC
      ) platforms
    ) as platform_distribution
  FROM cli_usage_events
  WHERE timestamp BETWEEN start_date AND end_date;
END;
$$; CREATE OR REPLACE FUNCTION public.search_blueprints_enhanced(search_query text = NULL, category_slug text = NULL, tags text[] = '{}', platforms text[] = '{}', complexity text = NULL, scope text = NULL, audience text = NULL, maturity text = NULL, framework text = NULL, language text = NULL, stack text = NULL) RETURNS SETOF public.blueprints LANGUAGE plpgsql VOLATILE AS $$
BEGIN
  RETURN QUERY
  SELECT DISTINCT r.*
  FROM public.blueprints r
  LEFT JOIN public.categories c ON r.category_id = c.id
  LEFT JOIN public.blueprint_platforms rp ON r.id = rp.blueprint_id
  WHERE
    (search_query IS NULL OR
     to_tsvector('english', r.title || ' ' || r.description || ' ' || r.content) @@ plainto_tsquery('english', search_query)) AND
    (category_slug IS NULL OR c.slug = category_slug) AND
    (array_length(tags, 1) IS NULL OR r.tags && tags) AND
    (array_length(platforms, 1) IS NULL OR EXISTS (
      SELECT 1 FROM public.blueprint_platforms rp2
      WHERE rp2.blueprint_id = r.id AND rp2.platform_type = ANY(platforms) AND rp2.is_compatible = true
    )) AND
    (complexity IS NULL OR r.complexity = complexity) AND
    (scope IS NULL OR r.scope = scope) AND
    (audience IS NULL OR r.audience = audience) AND
    (maturity IS NULL OR r.maturity = maturity) AND
    (framework IS NULL OR r.framework = framework) AND
    (language IS NULL OR r.language = language) AND
    (stack IS NULL OR r.stack = stack)
  ORDER BY
    CASE WHEN search_query IS NOT NULL
    THEN ts_rank(to_tsvector('english', r.title || ' ' || r.description || ' ' || r.content), plainto_tsquery('english', search_query))
    ELSE 0 END DESC,
    r.votes DESC,
    r.downloads DESC;
END;
$$; CREATE OR REPLACE FUNCTION increment_blueprint_usage(blueprint_id text) RETURNS void LANGUAGE plpgsql VOLATILE AS $$
BEGIN
    UPDATE blueprints
    SET downloads = COALESCE(downloads, 0) + 1,
        updated_at = CURRENT_TIMESTAMP
    WHERE id = blueprint_id;
END;
$$; CREATE OR REPLACE FUNCTION public.remove_blueprint_vote(target_blueprint_id text) RETURNS void LANGUAGE plpgsql VOLATILE AS $$
BEGIN
  DELETE FROM public.user_votes
  WHERE user_id = auth.uid() AND blueprint_id = target_blueprint_id;
  UPDATE public.blueprints
  SET votes = (SELECT COUNT(*) FROM public.user_votes WHERE user_votes.blueprint_id = target_blueprint_id)
  WHERE id = target_blueprint_id;
END;
$$; CREATE OR REPLACE FUNCTION public.get_blueprint_relationships(blueprint_id text, relationship_types text[] = '{"requires", "suggests", "conflicts", "supersedes", "enhances"}') RETURNS TABLE (relationship_type text, related_blueprint jsonb, condition_tags jsonb, condition_platforms text[], condition_stack text, priority int) LANGUAGE plpgsql VOLATILE AS $$
BEGIN
  RETURN QUERY
  SELECT
    rr.relationship_type,
    to_jsonb(related_blueprint.*) as related_blueprint,
    rr.condition_tags,
    rr.condition_platforms,
    rr.condition_stack,
    rr.priority
  FROM public.blueprint_relationships rr
  JOIN public.blueprints related_blueprint ON rr.related_blueprint_id = related_blueprint.id
  WHERE rr.blueprint_id = blueprint_id
    AND rr.relationship_type = ANY(relationship_types)
  ORDER BY rr.priority DESC, rr.relationship_type;
END;
$$; CREATE OR REPLACE FUNCTION upsert_user_blueprint_usage(p_user_id uuid, p_blueprint_id text) RETURNS void LANGUAGE plpgsql VOLATILE AS $$
BEGIN
    INSERT INTO user_blueprint_usage (user_id, blueprint_id, usage_count, last_used)
    VALUES (p_user_id, p_blueprint_id, 1, CURRENT_TIMESTAMP)
    ON CONFLICT (user_id, blueprint_id)
    DO UPDATE SET
        usage_count = user_blueprint_usage.usage_count + 1,
        last_used = CURRENT_TIMESTAMP,
        updated_at = CURRENT_TIMESTAMP;
END;
$$; CREATE OR REPLACE FUNCTION trigger_sync_vote_count() RETURNS trigger LANGUAGE plpgsql VOLATILE AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    UPDATE public.blueprints
    SET votes = votes + 1
    WHERE id = NEW.blueprint_id;
  ELSIF TG_OP = 'DELETE' THEN
    UPDATE public.blueprints
    SET votes = votes - 1
    WHERE id = OLD.blueprint_id;
  END IF;
  RETURN NULL;
END;
$$; CREATE OR REPLACE FUNCTION upsert_user_command_stats(p_user_id uuid, p_command text) RETURNS void LANGUAGE plpgsql VOLATILE AS $$
BEGIN
    INSERT INTO user_command_stats (user_id, command, usage_count, last_used)
    VALUES (p_user_id, p_command, 1, CURRENT_TIMESTAMP)
    ON CONFLICT (user_id, command)
    DO UPDATE SET
        usage_count = user_command_stats.usage_count + 1,
        last_used = CURRENT_TIMESTAMP,
        updated_at = CURRENT_TIMESTAMP;
END;
$$; CREATE OR REPLACE FUNCTION trigger_update_blueprint_timestamp() RETURNS trigger LANGUAGE plpgsql VOLATILE AS $$
BEGIN
  IF OLD.content IS DISTINCT FROM NEW.content THEN
    NEW.last_updated = NOW();
  END IF;
  RETURN NEW;
END;
$$; CREATE OR REPLACE FUNCTION public.get_popular_blueprints(limit_count int = 10) RETURNS SETOF public.blueprints LANGUAGE plpgsql VOLATILE AS $$
BEGIN
  RETURN QUERY
  SELECT r.*
  FROM public.blueprints r
  ORDER BY
    (r.votes * 2 + r.downloads) DESC,
    r.created_at DESC
  LIMIT limit_count;
END;
$$; CREATE OR REPLACE FUNCTION public.is_admin() RETURNS boolean LANGUAGE plpgsql VOLATILE AS $$
BEGIN
  RETURN auth.email() IN (SELECT email FROM public.admins);
END;
$$; CREATE OR REPLACE FUNCTION recalculate_blueprint_stats(blueprint_uuid uuid) RETURNS void LANGUAGE plpgsql VOLATILE AS $$
DECLARE
  new_usage_count INTEGER;
  new_success_rate DECIMAL(3,2);
  new_rating DECIMAL(3,2);
  new_vote_count INTEGER;
BEGIN
  SELECT COUNT(*) INTO new_usage_count
  FROM blueprint_usage
  WHERE blueprint_id = blueprint_uuid;
  SELECT 
    CASE 
      WHEN COUNT(*) = 0 THEN 0
      ELSE ROUND(COUNT(*) FILTER (WHERE success = true)::DECIMAL / COUNT(*), 2)
    END INTO new_success_rate
  FROM blueprint_usage
  WHERE blueprint_id = blueprint_uuid;
  SELECT 
    CASE 
      WHEN COUNT(*) FILTER (WHERE rating IS NOT NULL) = 0 THEN 0
      ELSE ROUND(AVG(rating), 2)
    END INTO new_rating
  FROM blueprint_votes
  WHERE blueprint_id = blueprint_uuid;
  SELECT COUNT(*) INTO new_vote_count
  FROM blueprint_votes
  WHERE blueprint_id = blueprint_uuid AND vote_type = 'up';
  UPDATE community_blueprints
  SET 
    usage_count = new_usage_count,
    deployment_success_rate = new_success_rate,
    rating = new_rating,
    vote_count = new_vote_count,
    updated_at = NOW()
  WHERE id = blueprint_uuid;
END;
$$; CREATE OR REPLACE FUNCTION public.get_blueprint_for_platform(blueprint_id text, platform text) RETURNS TABLE (blueprint_data jsonb, platform_config jsonb, commands jsonb) LANGUAGE plpgsql VOLATILE AS $$
BEGIN
  RETURN QUERY
  SELECT
    to_jsonb(r.*) as blueprint_data,
    COALESCE(rp.platform_config, '{}'::jsonb) as platform_config,
    COALESCE(
      (SELECT jsonb_agg(
        jsonb_build_object(
          'name', pc.command_name,
          'content', pc.command_content,
          'metadata', pc.command_metadata
        )
      ) FROM public.platform_commands pc
      WHERE pc.blueprint_id = r.id AND pc.platform_type = platform AND pc.is_active = true),
      '[]'::jsonb
    ) as commands
  FROM public.blueprints r
  LEFT JOIN public.blueprint_platforms rp ON r.id = rp.blueprint_id AND rp.platform_type = platform
  WHERE r.id = blueprint_id;
END;
$$; CREATE OR REPLACE FUNCTION generate_api_token() RETURNS text LANGUAGE plpgsql VOLATILE AS $$
DECLARE
    token_length INTEGER := 64;
    chars TEXT := 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
    result TEXT := '';
    i INTEGER := 0;
BEGIN
    FOR i IN 1..token_length LOOP
        result := result || substring(chars, floor(random() * char_length(chars) + 1)::integer, 1);
    END LOOP;
    RETURN 'vdk_' || result;
END;
$$; CREATE OR REPLACE FUNCTION get_cli_integration_summary(timeframe_days int = 7) RETURNS TABLE (integration_type text, total_events bigint, detections bigint, activations bigint, errors bigint, error_rate numeric) LANGUAGE plpgsql SECURITY DEFINER VOLATILE AS $$
BEGIN
  RETURN QUERY
  SELECT
    i.integration_type,
    COUNT(*) as total_events,
    COUNT(*) FILTER (WHERE action = 'detected') as detections,
    COUNT(*) FILTER (WHERE action = 'activated') as activations,
    COUNT(*) FILTER (WHERE success = false) as errors,
    (COUNT(*) FILTER (WHERE success = false))::NUMERIC / NULLIF(COUNT(*), 0) * 100 as error_rate
  FROM cli_integration_events i
  WHERE timestamp >= NOW() - (timeframe_days || ' days')::INTERVAL
  GROUP BY i.integration_type
  ORDER BY total_events DESC;
END;
$$; CREATE OR REPLACE FUNCTION validate_api_token(p_token text) RETURNS TABLE (user_id uuid, token_id uuid, is_valid boolean) LANGUAGE plpgsql VOLATILE AS $$
DECLARE
    found_user_id UUID;
    found_token_id UUID;
    is_expired BOOLEAN;
    is_active BOOLEAN;
BEGIN
    SELECT t.user_id, t.id, t.is_active, (t.expires_at < CURRENT_TIMESTAMP)
    INTO found_user_id, found_token_id, is_active, is_expired
    FROM user_api_tokens t
    WHERE (t.token_hash = p_token OR t.token_hash = crypt(p_token, t.token_hash))
      AND t.is_active = TRUE
      AND (t.expires_at IS NULL OR t.expires_at > CURRENT_TIMESTAMP)
    LIMIT 1;
    IF found_user_id IS NOT NULL AND NOT is_expired AND is_active THEN
        UPDATE user_api_tokens
        SET last_used = CURRENT_TIMESTAMP
        WHERE id = found_token_id;
        RETURN QUERY SELECT found_user_id, found_token_id, TRUE;
    ELSE
        RETURN QUERY SELECT NULL::UUID, NULL::UUID, FALSE;
    END IF;
END;
$$; CREATE OR REPLACE FUNCTION trigger_recalculate_stats_usage() RETURNS trigger LANGUAGE plpgsql VOLATILE AS $$
BEGIN
  PERFORM recalculate_blueprint_stats(NEW.blueprint_id);
  RETURN NEW;
END;
$$; CREATE OR REPLACE FUNCTION trigger_recalculate_stats_votes() RETURNS trigger LANGUAGE plpgsql VOLATILE AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    PERFORM recalculate_blueprint_stats(OLD.blueprint_id);
    RETURN OLD;
  ELSE
    PERFORM recalculate_blueprint_stats(NEW.blueprint_id);
    RETURN NEW;
  END IF;
END;
$$; CREATE OR REPLACE FUNCTION create_user_api_token(p_user_id uuid, p_token_name text = 'CLI Token', p_expires_in_days int = 365) RETURNS TABLE (token text, token_id uuid, prefix text) LANGUAGE plpgsql VOLATILE AS $$
DECLARE
    new_token TEXT;
    token_id UUID;
    token_prefix TEXT;
    token_hash TEXT;
BEGIN
    new_token := generate_api_token();
    token_prefix := LEFT(new_token, 8) || '...';
    BEGIN
        token_hash := crypt(new_token, gen_salt('bf'));
    EXCEPTION
        WHEN undefined_function THEN
            token_hash := new_token;
    END;
    INSERT INTO user_api_tokens (user_id, token_name, token_hash, token_prefix, expires_at)
    VALUES (
        p_user_id,
        p_token_name,
        token_hash,
        token_prefix,
        CURRENT_TIMESTAMP + (p_expires_in_days * INTERVAL '1 day')
    )
    RETURNING id INTO token_id;
    RETURN QUERY SELECT new_token, token_id, token_prefix;
END;
$$; CREATE TRIGGER trigger_platform_commands_set_updated_at BEFORE UPDATE ON public.platform_commands FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at(); CREATE TRIGGER update_user_blueprint_usage_updated_at BEFORE UPDATE ON user_blueprint_usage FOR EACH ROW EXECUTE FUNCTION update_updated_at_column(); CREATE TRIGGER trigger_commands_set_updated_at BEFORE UPDATE ON public.commands FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at(); CREATE TRIGGER update_user_command_stats_updated_at BEFORE UPDATE ON user_command_stats FOR EACH ROW EXECUTE FUNCTION update_updated_at_column(); CREATE TRIGGER trigger_blueprints_set_updated_at BEFORE UPDATE ON public.blueprints FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at(); CREATE TRIGGER trigger_generation_templates_set_updated_at BEFORE UPDATE ON public.generation_templates FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at(); CREATE TRIGGER update_user_project_stats_updated_at BEFORE UPDATE ON user_project_stats FOR EACH ROW EXECUTE FUNCTION update_updated_at_column(); CREATE TRIGGER update_user_api_tokens_updated_at BEFORE UPDATE ON user_api_tokens FOR EACH ROW EXECUTE FUNCTION update_updated_at_column(); CREATE TRIGGER update_team_configurations_updated_at BEFORE UPDATE ON team_configurations FOR EACH ROW EXECUTE FUNCTION update_updated_at_column(); CREATE TRIGGER update_cli_deployments_updated_at BEFORE UPDATE ON cli_deployments FOR EACH ROW EXECUTE FUNCTION update_updated_at_column(); CREATE TRIGGER update_vdk_versions_updated_at BEFORE UPDATE ON vdk_versions FOR EACH ROW EXECUTE FUNCTION update_updated_at_column(); CREATE TRIGGER trigger_categories_set_updated_at BEFORE UPDATE ON public.categories FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at(); CREATE TRIGGER update_user_platform_stats_updated_at BEFORE UPDATE ON user_platform_stats FOR EACH ROW EXECUTE FUNCTION update_updated_at_column(); CREATE TRIGGER trigger_blueprint_content_update BEFORE UPDATE ON public.blueprints FOR EACH ROW EXECUTE FUNCTION trigger_update_blueprint_timestamp(); CREATE TRIGGER trigger_blueprint_platforms_set_updated_at BEFORE UPDATE ON public.blueprint_platforms FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at(); CREATE TRIGGER update_blueprint_votes_updated_at BEFORE UPDATE ON blueprint_votes FOR EACH ROW EXECUTE FUNCTION update_updated_at_column(); CREATE TRIGGER update_community_blueprints_updated_at BEFORE UPDATE ON community_blueprints FOR EACH ROW EXECUTE FUNCTION update_updated_at_column(); CREATE TRIGGER trigger_vote_count_update AFTER INSERT OR DELETE ON public.user_votes FOR EACH ROW EXECUTE FUNCTION trigger_sync_vote_count(); CREATE TRIGGER recalculate_stats_on_votes AFTER INSERT OR DELETE OR UPDATE ON blueprint_votes FOR EACH ROW EXECUTE FUNCTION trigger_recalculate_stats_votes(); CREATE TRIGGER trigger_profiles_set_updated_at BEFORE UPDATE ON public.profiles FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at(); CREATE TRIGGER trigger_collections_set_updated_at BEFORE UPDATE ON public.collections FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at(); CREATE TRIGGER recalculate_stats_on_usage AFTER INSERT ON blueprint_usage FOR EACH ROW EXECUTE FUNCTION trigger_recalculate_stats_usage(); CREATE INDEX IF NOT EXISTS idx_blueprint_usage_blueprint_id ON blueprint_usage USING btree (blueprint_id); CREATE INDEX IF NOT EXISTS idx_blueprint_relationships_related_id ON public.blueprint_relationships USING btree (related_blueprint_id); CREATE INDEX IF NOT EXISTS idx_user_api_tokens_token_hash ON user_api_tokens USING btree (token_hash); CREATE INDEX IF NOT EXISTS idx_cli_recommendations_recommendation_id ON cli_recommendations USING btree (recommendation_id); CREATE INDEX idx_cli_performance_events_timestamp ON cli_performance_events USING btree ("timestamp"); CREATE INDEX idx_cli_error_events_error_type ON cli_error_events USING btree (error_type); CREATE INDEX IF NOT EXISTS idx_cli_deployments_deployment_id ON cli_deployments USING btree (deployment_id); CREATE INDEX IF NOT EXISTS idx_blueprints_language ON public.blueprints USING btree (language); CREATE INDEX IF NOT EXISTS idx_team_configurations_user_id ON team_configurations USING btree (user_id); CREATE INDEX IF NOT EXISTS idx_blueprints_frontmatter ON public.blueprints USING gin (frontmatter); CREATE INDEX IF NOT EXISTS idx_vdk_versions_status ON vdk_versions USING btree (status); CREATE INDEX IF NOT EXISTS idx_commands_category ON public.commands USING btree (category); CREATE INDEX IF NOT EXISTS idx_community_blueprints_search ON community_blueprints USING gin (to_tsvector('english', (((title || ' ') || COALESCE(description, '')) || ' ') || COALESCE(tags::text, ''))); CREATE INDEX idx_collection_items_item_type ON collection_items USING btree (item_type); CREATE INDEX IF NOT EXISTS idx_blueprint_compatibility_technology ON public.blueprint_compatibility USING btree (technology); CREATE INDEX IF NOT EXISTS idx_community_blueprints_platforms ON community_blueprints USING gin (platforms); CREATE INDEX IF NOT EXISTS idx_user_blueprint_usage_blueprint_id ON user_blueprint_usage USING btree (blueprint_id); CREATE INDEX IF NOT EXISTS idx_user_platform_stats_user_id ON user_platform_stats USING btree (user_id); CREATE INDEX IF NOT EXISTS idx_blueprints_license ON public.blueprints USING btree (license) WHERE license IS NOT NULL; CREATE INDEX idx_cli_error_events_session_id ON cli_error_events USING btree (session_id); CREATE INDEX idx_cli_error_events_timestamp ON cli_error_events USING btree ("timestamp"); CREATE INDEX IF NOT EXISTS idx_blueprints_stack ON public.blueprints USING btree (stack); CREATE INDEX idx_cli_performance_events_session_id ON cli_performance_events USING btree (session_id); CREATE INDEX idx_cli_integration_events_session_id ON cli_integration_events USING btree (session_id); CREATE INDEX IF NOT EXISTS idx_generation_templates_active ON public.generation_templates USING btree (is_active) WHERE is_active = true; CREATE INDEX idx_cli_usage_events_session_id ON cli_usage_events USING btree (session_id); CREATE INDEX IF NOT EXISTS idx_collections_user_id ON public.collections USING btree (user_id); CREATE INDEX IF NOT EXISTS idx_cli_deployments_deployed_at ON cli_deployments USING btree (deployed_at); CREATE INDEX idx_cli_integration_events_action ON cli_integration_events USING btree (action); CREATE INDEX IF NOT EXISTS idx_generated_packages_expires_at ON public.generated_packages USING btree (expires_at); CREATE INDEX IF NOT EXISTS idx_commands_target ON public.commands USING btree (target); CREATE INDEX IF NOT EXISTS idx_categories_type ON public.categories USING btree (category_type); CREATE INDEX IF NOT EXISTS idx_community_blueprints_created_at ON community_blueprints USING btree (created_at DESC); CREATE INDEX IF NOT EXISTS idx_cli_deployments_project_name ON cli_deployments USING btree (project_name); CREATE INDEX IF NOT EXISTS idx_categories_parent_id ON public.categories USING btree (parent_id); CREATE INDEX idx_collection_items_command_id ON collection_items USING btree (command_id); CREATE INDEX IF NOT EXISTS idx_user_blueprint_usage_last_used ON user_blueprint_usage USING btree (last_used); CREATE INDEX IF NOT EXISTS idx_vdk_analytics_created_at ON vdk_analytics USING btree (created_at); CREATE INDEX IF NOT EXISTS idx_community_blueprints_status_category ON community_blueprints USING btree (status, category); CREATE INDEX IF NOT EXISTS idx_commands_active ON public.commands USING btree (is_active) WHERE is_active = true; CREATE INDEX IF NOT EXISTS idx_user_api_tokens_is_active ON user_api_tokens USING btree (is_active); CREATE INDEX IF NOT EXISTS idx_vdk_error_logs_error_type ON vdk_error_logs USING btree (error_type); CREATE INDEX IF NOT EXISTS idx_blueprints_content_search ON public.blueprints USING gin (to_tsvector('english', (((title || ' ') || description) || ' ') || content)); CREATE INDEX idx_cli_usage_events_command ON cli_usage_events USING btree (command); CREATE INDEX IF NOT EXISTS idx_vdk_versions_component ON vdk_versions USING btree (component); CREATE INDEX IF NOT EXISTS idx_blueprint_usage_deployed_at ON blueprint_usage USING btree (deployed_at DESC); CREATE INDEX IF NOT EXISTS idx_cli_deployment_blueprints_blueprint_name ON cli_deployment_blueprints USING btree (blueprint_name); CREATE INDEX idx_cli_performance_events_command ON cli_performance_events USING btree (command); CREATE INDEX IF NOT EXISTS idx_commands_scope ON public.commands USING btree (scope); CREATE INDEX IF NOT EXISTS idx_platform_commands_active ON public.platform_commands USING btree (is_active) WHERE is_active = true; CREATE INDEX IF NOT EXISTS idx_blueprint_usage_session_id ON blueprint_usage USING btree (session_id); CREATE INDEX IF NOT EXISTS idx_blueprint_usage_platform ON blueprint_usage USING btree (platform); CREATE INDEX IF NOT EXISTS idx_wizard_configurations_user_id ON public.wizard_configurations USING btree (user_id); CREATE INDEX IF NOT EXISTS idx_blueprint_relationships_blueprint_id ON public.blueprint_relationships USING btree (blueprint_id); CREATE INDEX IF NOT EXISTS idx_community_blueprints_published_at ON community_blueprints USING btree (published_at DESC); CREATE INDEX IF NOT EXISTS idx_blueprint_platforms_compatible ON public.blueprint_platforms USING btree (is_compatible) WHERE is_compatible = true; CREATE INDEX idx_cli_usage_events_timestamp ON cli_usage_events USING btree ("timestamp"); CREATE INDEX IF NOT EXISTS idx_blueprints_category_id ON public.blueprints USING btree (category_id); CREATE INDEX IF NOT EXISTS idx_blueprints_maturity ON public.blueprints USING btree (maturity); CREATE INDEX IF NOT EXISTS idx_blueprints_author ON public.blueprints USING btree (author); CREATE INDEX IF NOT EXISTS idx_community_blueprints_status_rating ON community_blueprints USING btree (status, rating DESC); CREATE INDEX IF NOT EXISTS idx_blueprints_framework ON public.blueprints USING btree (framework); CREATE INDEX IF NOT EXISTS idx_collection_items_collection_id ON public.collection_items USING btree (collection_id); CREATE INDEX IF NOT EXISTS idx_categories_slug ON public.categories USING btree (slug); CREATE INDEX IF NOT EXISTS idx_blueprints_complexity ON public.blueprints USING btree (complexity); CREATE INDEX IF NOT EXISTS idx_community_blueprints_language ON community_blueprints USING btree (language); CREATE INDEX IF NOT EXISTS idx_team_configurations_team_id ON team_configurations USING btree (team_id); CREATE INDEX IF NOT EXISTS idx_blueprints_tags ON public.blueprints USING gin (tags); CREATE INDEX IF NOT EXISTS idx_blueprints_repository_url ON public.blueprints USING btree (repository_url) WHERE repository_url IS NOT NULL; CREATE INDEX IF NOT EXISTS idx_wizard_configurations_session_id ON public.wizard_configurations USING btree (session_id); CREATE INDEX idx_cli_integration_events_integration_type ON cli_integration_events USING btree (integration_type); CREATE INDEX IF NOT EXISTS idx_user_project_stats_user_id ON user_project_stats USING btree (user_id); CREATE INDEX IF NOT EXISTS idx_blueprint_usage_blueprint_deployed ON blueprint_usage USING btree (blueprint_id, deployed_at DESC); CREATE INDEX IF NOT EXISTS idx_community_blueprints_framework ON community_blueprints USING btree (framework); CREATE INDEX IF NOT EXISTS idx_blueprints_scope ON public.blueprints USING btree (scope); CREATE INDEX IF NOT EXISTS idx_community_blueprints_rating ON community_blueprints USING btree (rating DESC); CREATE INDEX idx_cli_usage_events_user_id ON cli_usage_events USING btree (user_id) WHERE user_id IS NOT NULL; CREATE INDEX IF NOT EXISTS idx_blueprint_versions_blueprint_id ON public.blueprint_versions USING btree (blueprint_id); CREATE INDEX IF NOT EXISTS idx_commands_command_type ON public.commands USING btree (command_type); CREATE INDEX IF NOT EXISTS idx_commands_tags ON public.commands USING gin (tags); CREATE INDEX IF NOT EXISTS idx_community_blueprints_status ON community_blueprints USING btree (status); CREATE INDEX IF NOT EXISTS idx_team_configurations_updated_at ON team_configurations USING btree (updated_at); CREATE INDEX IF NOT EXISTS idx_cli_deployment_blueprints_deployment_id ON cli_deployment_blueprints USING btree (deployment_id); CREATE INDEX IF NOT EXISTS idx_blueprint_votes_vote_type ON blueprint_votes USING btree (vote_type); CREATE INDEX IF NOT EXISTS idx_platform_commands_platform ON public.platform_commands USING btree (platform_type); CREATE INDEX IF NOT EXISTS idx_blueprint_usage_framework ON blueprint_usage USING btree (framework); CREATE INDEX IF NOT EXISTS idx_vdk_analytics_project_id ON vdk_analytics USING btree (project_id); CREATE INDEX IF NOT EXISTS idx_blueprints_subcategory ON public.blueprints USING btree (subcategory); CREATE INDEX IF NOT EXISTS idx_blueprints_always_apply ON public.blueprints USING btree (always_apply) WHERE always_apply = true; CREATE INDEX IF NOT EXISTS idx_user_api_tokens_expires_at ON user_api_tokens USING btree (expires_at); CREATE INDEX IF NOT EXISTS idx_user_command_stats_command ON user_command_stats USING btree (command); CREATE INDEX IF NOT EXISTS idx_blueprint_platforms_blueprint_id ON public.blueprint_platforms USING btree (blueprint_id); CREATE INDEX IF NOT EXISTS idx_blueprint_relationships_platforms ON public.blueprint_relationships USING gin (condition_platforms); CREATE INDEX IF NOT EXISTS idx_user_command_stats_user_id ON user_command_stats USING btree (user_id); CREATE INDEX IF NOT EXISTS idx_blueprint_compatibility_blueprint_id ON public.blueprint_compatibility USING btree (blueprint_id); CREATE INDEX idx_cli_performance_events_operation ON cli_performance_events USING btree (operation); CREATE INDEX IF NOT EXISTS idx_vdk_error_logs_created_at ON vdk_error_logs USING btree (created_at); CREATE INDEX IF NOT EXISTS idx_vdk_analytics_user_id ON vdk_analytics USING btree (user_id); CREATE INDEX IF NOT EXISTS idx_blueprint_platforms_platform_type ON public.blueprint_platforms USING btree (platform_type); CREATE INDEX idx_cli_integration_events_timestamp ON cli_integration_events USING btree ("timestamp"); CREATE INDEX IF NOT EXISTS idx_commands_blueprint_id ON public.commands USING btree (blueprint_id); CREATE INDEX IF NOT EXISTS idx_user_project_stats_project_id ON user_project_stats USING btree (project_id); CREATE INDEX IF NOT EXISTS idx_collection_items_blueprint_id ON public.collection_items USING btree (blueprint_id); CREATE INDEX IF NOT EXISTS idx_user_blueprint_usage_user_id ON user_blueprint_usage USING btree (user_id); CREATE INDEX IF NOT EXISTS idx_cli_deployments_team_name ON cli_deployments USING btree (team_name); CREATE INDEX IF NOT EXISTS idx_user_api_tokens_user_id ON user_api_tokens USING btree (user_id); CREATE INDEX IF NOT EXISTS idx_blueprint_usage_success ON blueprint_usage USING btree (success); CREATE INDEX IF NOT EXISTS idx_platform_commands_blueprint_id ON public.platform_commands USING btree (blueprint_id); CREATE INDEX IF NOT EXISTS idx_user_platform_stats_platform ON user_platform_stats USING btree (platform); CREATE INDEX IF NOT EXISTS idx_generated_packages_configuration_id ON public.generated_packages USING btree (configuration_id); CREATE INDEX IF NOT EXISTS idx_blueprint_platforms_config ON public.blueprint_platforms USING gin (platform_config); CREATE INDEX IF NOT EXISTS idx_categories_platform_specific ON public.categories USING btree (platform_specific); CREATE INDEX IF NOT EXISTS idx_blueprint_dependencies_blueprint_id ON public.blueprint_dependencies USING btree (blueprint_id); CREATE INDEX IF NOT EXISTS idx_blueprints_schema_version ON public.blueprints USING btree (schema_version) WHERE schema_version IS NOT NULL; CREATE INDEX IF NOT EXISTS idx_blueprints_contributors ON public.blueprints USING gin (contributors); CREATE INDEX IF NOT EXISTS idx_vdk_error_logs_user_id ON vdk_error_logs USING btree (user_id); CREATE INDEX IF NOT EXISTS idx_community_blueprints_author_username ON community_blueprints USING btree (author_username); CREATE INDEX IF NOT EXISTS idx_blueprint_validations_blueprint_id ON public.blueprint_validations USING btree (blueprint_id); CREATE INDEX idx_cli_error_events_command ON cli_error_events USING btree (command); CREATE INDEX IF NOT EXISTS idx_community_blueprints_usage_count ON community_blueprints USING btree (usage_count DESC); CREATE INDEX IF NOT EXISTS idx_blueprint_votes_user_id ON blueprint_votes USING btree (user_id); CREATE INDEX IF NOT EXISTS idx_community_blueprints_category ON community_blueprints USING btree (category); CREATE INDEX IF NOT EXISTS idx_community_blueprints_status_usage ON community_blueprints USING btree (status, usage_count DESC); CREATE INDEX IF NOT EXISTS idx_vdk_versions_version ON vdk_versions USING btree (version); CREATE INDEX IF NOT EXISTS idx_blueprints_slug ON public.blueprints USING btree (slug); CREATE INDEX IF NOT EXISTS idx_cli_recommendations_created_at ON cli_recommendations USING btree (created_at); CREATE INDEX IF NOT EXISTS idx_blueprints_audience ON public.blueprints USING btree (audience); CREATE INDEX IF NOT EXISTS idx_blueprint_votes_blueprint_id ON blueprint_votes USING btree (blueprint_id); CREATE INDEX IF NOT EXISTS idx_blueprint_votes_rating ON blueprint_votes USING btree (rating); CREATE INDEX idx_cli_usage_events_platform ON cli_usage_events USING btree (platform); CREATE INDEX IF NOT EXISTS idx_blueprint_dependencies_depends_on ON public.blueprint_dependencies USING btree (depends_on_blueprint_id); CREATE INDEX IF NOT EXISTS idx_blueprint_validations_validated_at ON public.blueprint_validations USING btree (validated_at); CREATE INDEX IF NOT EXISTS idx_blueprint_votes_created_at ON blueprint_votes USING btree (created_at DESC); CREATE INDEX IF NOT EXISTS idx_categories_supported_platforms ON public.categories USING gin (supported_platforms); CREATE INDEX IF NOT EXISTS idx_blueprint_relationships_type ON public.blueprint_relationships USING btree (relationship_type); CREATE INDEX IF NOT EXISTS idx_blueprint_validations_status ON public.blueprint_validations USING btree (validation_status); CREATE INDEX IF NOT EXISTS idx_community_blueprints_tags ON community_blueprints USING gin (tags); CREATE INDEX IF NOT EXISTS idx_vdk_analytics_event_type ON vdk_analytics USING btree (event_type); CREATE POLICY "Allow updating download counts" ON public.generated_packages FOR UPDATE TO public USING (true) WITH CHECK (true) ; CREATE POLICY "Admins can manage all data" ON public.categories TO public USING (auth.email() IN (SELECT email FROM public.admins)) ; CREATE POLICY "Admins can manage blueprint relationships" ON public.blueprint_relationships TO public USING (auth.email() IN (SELECT email FROM public.admins)) ; CREATE POLICY "Users can insert their own blueprint usage" ON user_blueprint_usage FOR INSERT TO public WITH CHECK (auth.uid()::text = user_id::text) ; CREATE POLICY "Users can update their own command stats" ON user_command_stats FOR UPDATE TO public USING (auth.uid()::text = user_id::text) ; CREATE POLICY "Service role full access" ON community_blueprints TO public USING ((auth.jwt() ->> 'role') = 'service_role') ; CREATE POLICY "Authenticated users can insert CLI error events" ON cli_error_events FOR INSERT TO public WITH CHECK (auth.role() = 'authenticated' OR auth.role() = 'anon') ; CREATE POLICY "Authenticated users can insert CLI integration events" ON cli_integration_events FOR INSERT TO public WITH CHECK (auth.role() = 'authenticated' OR auth.role() = 'anon') ; CREATE POLICY "Anonymous users can read generated packages" ON public.generated_packages FOR SELECT TO public USING (true) ; CREATE POLICY "Anyone can insert usage records" ON blueprint_usage FOR INSERT TO public WITH CHECK (true) ; CREATE POLICY "Service role full access on usage" ON blueprint_usage TO public USING ((auth.jwt() ->> 'role') = 'service_role') ; CREATE POLICY "Authenticated users can insert CLI usage events" ON cli_usage_events FOR INSERT TO public WITH CHECK (auth.role() = 'authenticated' OR auth.role() = 'anon') ; CREATE POLICY "Service role can access all CLI performance events" ON cli_performance_events TO public USING (auth.role() = 'service_role') ; CREATE POLICY "Users can insert their own team configurations" ON team_configurations FOR INSERT TO public WITH CHECK (auth.uid()::text = user_id::text) ; CREATE POLICY "Users can insert their own analytics" ON vdk_analytics FOR INSERT TO public WITH CHECK (auth.uid()::text = user_id::text) ; CREATE POLICY "Users can read items in public collections" ON public.collection_items FOR SELECT TO public USING (EXISTS (SELECT 1 FROM public.collections c WHERE c.id = collection_id AND (c.is_public OR c.user_id = auth.uid()))) ; CREATE POLICY "Users can upload their own avatars" ON storage.objects FOR INSERT TO public WITH CHECK (bucket_id = 'avatars' AND auth.uid()::text = (storage.foldername(name))[1]) ; CREATE POLICY "Service role can access all CLI usage events" ON cli_usage_events TO public USING (auth.role() = 'service_role') ; CREATE POLICY "Anonymous users can create generated packages" ON public.generated_packages FOR INSERT TO public WITH CHECK (true) ; CREATE POLICY "Users can update their own platform stats" ON user_platform_stats FOR UPDATE TO public USING (auth.uid()::text = user_id::text) ; CREATE POLICY "Public read access for avatars" ON storage.objects FOR SELECT TO public USING (bucket_id = 'avatars') ; CREATE POLICY "Public read access for active commands" ON public.commands FOR SELECT TO public USING (is_active = true) ; CREATE POLICY "Public read access for blueprint versions" ON public.blueprint_versions FOR SELECT TO public USING (true) ; CREATE POLICY "Authenticated users can view recommendations" ON cli_recommendations FOR SELECT TO public USING (auth.uid() IS NOT NULL) ; CREATE POLICY "Public read access for generated packages" ON storage.objects FOR SELECT TO public USING (bucket_id = 'generated-packages') ; CREATE POLICY "Users can update their own collection items with commands" ON collection_items FOR UPDATE TO public USING (collection_id IN (SELECT id FROM collections WHERE user_id = auth.uid())) ; CREATE POLICY "Users can delete their own avatars" ON storage.objects FOR DELETE TO public USING (bucket_id = 'avatars' AND auth.uid()::text = (storage.foldername(name))[1]) ; CREATE POLICY "Users can update their own team configurations" ON team_configurations FOR UPDATE TO public USING (auth.uid()::text = user_id::text) ; CREATE POLICY "Users can view their own blueprint usage" ON user_blueprint_usage FOR SELECT TO public USING (auth.uid()::text = user_id::text) ; CREATE POLICY "Public read access for published blueprints" ON community_blueprints FOR SELECT TO public USING (status = 'published') ; CREATE POLICY "Authenticated users can view deployments" ON cli_deployments FOR SELECT TO public USING (auth.uid() IS NOT NULL) ; CREATE POLICY "Users can view their own team configurations" ON team_configurations FOR SELECT TO public USING (auth.uid()::text = user_id::text) ; CREATE POLICY "Admins can manage blueprint platforms" ON public.blueprint_platforms TO public USING (auth.email() IN (SELECT email FROM public.admins)) ; CREATE POLICY "Authors can insert blueprints" ON community_blueprints FOR INSERT TO public WITH CHECK (auth.uid() = author_id) ; CREATE POLICY "Users can manage their own collections" ON public.collections TO public USING (auth.uid() = user_id) ; CREATE POLICY "Public read access for votes" ON blueprint_votes FOR SELECT TO public USING (true) ; CREATE POLICY "Users can delete their own API tokens" ON user_api_tokens FOR DELETE TO public USING (auth.uid() = user_id) ; CREATE POLICY "Users can manage their votes" ON public.user_votes TO public USING (auth.uid() = user_id) ; CREATE POLICY "Service role full access on votes" ON blueprint_votes TO public USING ((auth.jwt() ->> 'role') = 'service_role') ; CREATE POLICY "Authenticated users can insert CLI performance events" ON cli_performance_events FOR INSERT TO public WITH CHECK (auth.role() = 'authenticated' OR auth.role() = 'anon') ; CREATE POLICY "Authenticated users can insert deployments" ON cli_deployments FOR INSERT TO public WITH CHECK (auth.uid() IS NOT NULL) ; CREATE POLICY "Users can update their own avatars" ON storage.objects FOR UPDATE TO public USING (bucket_id = 'avatars' AND auth.uid()::text = (storage.foldername(name))[1]) ; CREATE POLICY "Public read access for blueprint relationships" ON public.blueprint_relationships FOR SELECT TO public USING (true) ; CREATE POLICY "Users can delete their own collection items with commands" ON collection_items FOR DELETE TO public USING (collection_id IN (SELECT id FROM collections WHERE user_id = auth.uid())) ; CREATE POLICY "Users can read their own wizard configs" ON public.wizard_configurations FOR SELECT TO public USING (auth.uid() = user_id OR user_id IS NULL) ; CREATE POLICY "Authors can read their own blueprints" ON community_blueprints FOR SELECT TO public USING (auth.uid() = author_id) ; CREATE POLICY "Anyone can view version information" ON vdk_versions FOR SELECT TO public USING (true) ; CREATE POLICY "Users can delete their own votes" ON blueprint_votes FOR DELETE TO public USING (auth.uid() = user_id) ; CREATE POLICY "Public read access for platform commands" ON public.platform_commands FOR SELECT TO public USING (true) ; CREATE POLICY "Admins can manage platform commands" ON public.platform_commands TO public USING (auth.email() IN (SELECT email FROM public.admins)) ; CREATE POLICY "Anyone can upload generated packages" ON storage.objects FOR INSERT TO public WITH CHECK (bucket_id = 'generated-packages') ; CREATE POLICY "Authenticated users can read profiles" ON public.profiles FOR SELECT TO public USING (auth.role() = 'authenticated') ; CREATE POLICY "Users can insert their own profile" ON public.profiles FOR INSERT TO public WITH CHECK (auth.uid() = id) ; CREATE POLICY "Authenticated users can insert recommendations" ON cli_recommendations FOR INSERT TO public WITH CHECK (auth.uid() IS NOT NULL) ; CREATE POLICY "Admins can manage all commands" ON public.commands TO public USING (EXISTS (SELECT 1 FROM public.admins a JOIN public.profiles p ON p.email = a.email WHERE p.id = auth.uid())) ; CREATE POLICY "Service role can access all CLI error events" ON cli_error_events TO public USING (auth.role() = 'service_role') ; CREATE POLICY "Admins can manage all blueprints" ON public.blueprints TO public USING (auth.email() IN (SELECT email FROM public.admins)) ; CREATE POLICY "Authenticated users can insert deployment blueprints" ON cli_deployment_blueprints FOR INSERT TO public WITH CHECK (auth.uid() IS NOT NULL) ; CREATE POLICY "Public read access for blueprint platforms" ON public.blueprint_platforms FOR SELECT TO public USING (true) ; CREATE POLICY "Users can insert their own API tokens" ON user_api_tokens FOR INSERT TO public WITH CHECK (auth.uid() = user_id) ; CREATE POLICY "Users can update their own profiles" ON public.profiles FOR UPDATE TO public USING (auth.uid() = id) ; CREATE POLICY "Users can insert their own platform stats" ON user_platform_stats FOR INSERT TO public WITH CHECK (auth.uid()::text = user_id::text) ; CREATE POLICY "Public read access for blueprint validations" ON public.blueprint_validations FOR SELECT TO public USING (true) ; CREATE POLICY "Users can manage items in their collections" ON public.collection_items TO public USING (EXISTS (SELECT 1 FROM public.collections c WHERE c.id = collection_id AND c.user_id = auth.uid())) ; CREATE POLICY "Users can view their own analytics" ON vdk_analytics FOR SELECT TO public USING (auth.uid()::text = user_id::text) ; CREATE POLICY "Service role can access all CLI integration events" ON cli_integration_events TO public USING (auth.role() = 'service_role') ; CREATE POLICY "Admins can manage blueprint validations" ON public.blueprint_validations TO public USING (auth.email() IN (SELECT email FROM public.admins)) ; CREATE POLICY "Users can update their own API tokens" ON user_api_tokens FOR UPDATE TO public USING (auth.uid() = user_id) ; CREATE POLICY "Users can insert their own project stats" ON user_project_stats FOR INSERT TO public WITH CHECK (auth.uid()::text = user_id::text) ; CREATE POLICY "Users can delete their own profiles" ON public.profiles FOR DELETE TO public USING (auth.uid() = id) ; CREATE POLICY "Users can insert their own votes" ON blueprint_votes FOR INSERT TO public WITH CHECK (auth.uid() = user_id) ; CREATE POLICY "Users can view their own platform stats" ON user_platform_stats FOR SELECT TO public USING (auth.uid()::text = user_id::text) ; CREATE POLICY "Public read access for blueprints" ON public.blueprints FOR SELECT TO public USING (true) ; CREATE POLICY "Users can read public collections" ON public.collections FOR SELECT TO public USING (is_public = true OR auth.uid() = user_id) ; CREATE POLICY "Public read access for usage analytics" ON blueprint_usage FOR SELECT TO public USING (true) ; CREATE POLICY "Users can update their own project stats" ON user_project_stats FOR UPDATE TO public USING (auth.uid()::text = user_id::text) ; CREATE POLICY "Users can view their own command stats" ON user_command_stats FOR SELECT TO public USING (auth.uid()::text = user_id::text) ; CREATE POLICY "Anonymous users can create wizard configs" ON public.wizard_configurations FOR INSERT TO public WITH CHECK (true) ; CREATE POLICY "Admins can view sync logs" ON public.sync_logs FOR SELECT TO public USING (auth.email() IN (SELECT email FROM public.admins)) ; CREATE POLICY "Users can create commands" ON public.commands FOR INSERT TO public WITH CHECK (auth.uid() IS NOT NULL AND is_active = false) ; CREATE POLICY "Allow package cleanup" ON storage.objects FOR DELETE TO public USING (bucket_id = 'generated-packages') ; CREATE POLICY "Users can insert their own error logs" ON vdk_error_logs FOR INSERT TO public WITH CHECK (auth.uid()::text = user_id::text) ; CREATE POLICY "Users can insert their own command stats" ON user_command_stats FOR INSERT TO public WITH CHECK (auth.uid()::text = user_id::text) ; CREATE POLICY "Users can insert their own collection items with commands" ON collection_items FOR INSERT TO public WITH CHECK (collection_id IN (SELECT id FROM collections WHERE user_id = auth.uid())) ; CREATE POLICY "Users can view their own collection items with commands" ON collection_items FOR SELECT TO public USING (collection_id IN (SELECT id FROM collections WHERE user_id = auth.uid())) ; CREATE POLICY "Authors can update their own blueprints" ON community_blueprints FOR UPDATE TO public USING (auth.uid() = author_id) ; CREATE POLICY "Users can view their own error logs" ON vdk_error_logs FOR SELECT TO public USING (auth.uid()::text = user_id::text) ; CREATE POLICY "Public read access for categories" ON public.categories FOR SELECT TO public USING (true) ; CREATE POLICY "Users can update their own votes" ON blueprint_votes FOR UPDATE TO public USING (auth.uid() = user_id) ; CREATE POLICY "Users can view their own API tokens" ON user_api_tokens FOR SELECT TO public USING (auth.uid() = user_id) ; CREATE POLICY "Users can view their own project stats" ON user_project_stats FOR SELECT TO public USING (auth.uid()::text = user_id::text) ; CREATE POLICY "Users can update their own blueprint usage" ON user_blueprint_usage FOR UPDATE TO public USING (auth.uid()::text = user_id::text) ; CREATE POLICY "Users can delete their own team configurations" ON team_configurations FOR DELETE TO public USING (auth.uid()::text = user_id::text) ; CREATE POLICY "Authenticated users can view deployment blueprints" ON cli_deployment_blueprints FOR SELECT TO public USING (auth.uid() IS NOT NULL) ; COMMENT ON COLUMN public.commands.claude_code_config IS 'Claude Code specific configuration including slashCommand, arguments, fileReferences, etc.'; COMMENT ON TABLE public.blueprint_relationships IS 'Enhanced blueprint dependency relationships with conditional logic'; COMMENT ON TABLE public.blueprint_validations IS 'Schema validation tracking for quality assurance'; COMMENT ON COLUMN collection_items.command_id IS 'Reference to commands table. Mutually exclusive with blueprint_id.'; COMMENT ON POLICY "Allow updating download counts" ON public.generated_packages IS 'Allows incrementing download counters for analytics and tracking'; COMMENT ON TABLE cli_performance_events IS 'Monitors CLI performance metrics for optimization'; COMMENT ON COLUMN public.commands.installation IS 'Installation requirements including dependencies and setup steps'; COMMENT ON TABLE collection_items IS 'Collection items can contain either blueprints or commands, but not both. Use item_type column to distinguish.'; COMMENT ON COLUMN public.commands.permissions IS 'Command permissions including allowedTools and requiredApproval'; COMMENT ON COLUMN public.blueprints.license IS 'License identifier, preferably SPDX format (AI Context Schema v2.1.0)'; COMMENT ON COLUMN public.blueprints.schema_version IS 'AI Context Schema version used by this blueprint (AI Context Schema v2.1.0)'; COMMENT ON POLICY "Anonymous users can create generated packages" ON public.generated_packages IS 'Allows anonymous users to create package records when generating blueprint packages from wizard'; COMMENT ON TABLE public.blueprint_platforms IS 'Platform-specific configurations for AI assistants (Claude Code, Cursor, Windsurf, GitHub Copilot)'; COMMENT ON TABLE cli_integration_events IS 'Tracks IDE and AI assistant integration events'; COMMENT ON TABLE public.platform_commands IS 'Platform-specific command implementations'; COMMENT ON TABLE public.blueprints IS 'Enhanced blueprints table supporting .ai/ structure with multi-platform AI assistant integration'; COMMENT ON COLUMN public.blueprints.compatibility IS 'Legacy compatibility field - migrating to platform_config'; COMMENT ON VIEW collection_contents IS 'Unified view of collection contents with both blueprints and commands data.'; COMMENT ON TABLE cli_error_events IS 'Records CLI errors for debugging and improvement'; COMMENT ON COLUMN collection_items.item_type IS 'Computed column indicating whether this item is a blueprint or command.'; COMMENT ON TABLE public.admins IS 'RLS disabled - security handled by is_admin() function and admin email verification'; COMMENT ON TABLE cli_usage_events IS 'Tracks VDK CLI command usage, execution times, and success rates'; COMMENT ON TABLE public.commands IS 'Claude Code commands with rich metadata based on command-schema.json'; COMMENT ON COLUMN public.blueprints.repository_url IS 'Source repository URL for the blueprint (AI Context Schema v2.1.0)'; COMMENT ON COLUMN public.commands.examples IS 'Array of usage examples with context and expected outcomes'; DO $$
BEGIN
  RAISE NOTICE 'Community Blueprint tables created successfully!';
  RAISE NOTICE 'Tables: community_blueprints, blueprint_usage, blueprint_votes';
  RAISE NOTICE 'Indexes: % total indexes created', (
    SELECT COUNT(*) 
    FROM pg_indexes 
    WHERE tablename IN ('community_blueprints', 'blueprint_usage', 'blueprint_votes')
  );
  RAISE NOTICE 'RLS policies: Enabled on all tables';
  RAISE NOTICE 'Sample data: 2 blueprints, 3 usage records, 3 votes inserted';
END $$