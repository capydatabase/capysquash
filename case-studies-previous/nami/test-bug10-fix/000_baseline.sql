CREATE SCHEMA IF NOT EXISTS auth; DO $$
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
$$; CREATE OR REPLACE FUNCTION auth.jwt() RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER STABLE AS $$
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
$$; CREATE OR REPLACE FUNCTION current_user_id() RETURNS text LANGUAGE plpgsql SECURITY DEFINER STABLE AS $$
BEGIN
  RETURN (auth.jwt() ->> 'sub')::text;
END;
$$; CREATE OR REPLACE FUNCTION current_organization_id() RETURNS text LANGUAGE plpgsql SECURITY DEFINER STABLE AS $$
BEGIN
  RETURN (auth.jwt()->'o'->>'id')::text;
END;
$$; CREATE OR REPLACE FUNCTION current_organization_role() RETURNS text LANGUAGE plpgsql SECURITY DEFINER STABLE AS $$
BEGIN
  RETURN (auth.jwt()->'o'->>'role')::text;
END;
$$; CREATE OR REPLACE FUNCTION current_organization_name() RETURNS text LANGUAGE plpgsql SECURITY DEFINER STABLE AS $$
BEGIN
  RETURN (auth.jwt()->'o'->>'name')::text;
END;
$$; DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_publication WHERE pubname = 'supabase_realtime') THEN
    CREATE PUBLICATION supabase_realtime;
  END IF;
END
$$; DO $$
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
$$; CREATE EXTENSION IF NOT EXISTS "uuid-ossp"; CREATE EXTENSION IF NOT EXISTS pg_trgm; CREATE EXTENSION IF NOT EXISTS pgcrypto; CREATE TABLE IF NOT EXISTS public.usage_analytics (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), date date NOT NULL, primary_support text, energy_level text, ui_state text, total_conversations int DEFAULT 0, total_tokens int DEFAULT 0, total_cost_usd numeric DEFAULT 0.00, tools_used jsonb DEFAULT ('[]'::jsonb), average_response_time numeric, error_count int DEFAULT 0, successful_interactions int DEFAULT 0, memory_cards_created int DEFAULT 0, crisis_interventions int DEFAULT 0, helpful_responses int DEFAULT 0, created_at timestamp with time zone DEFAULT now(), UNIQUE (date, primary_support, energy_level, ui_state)); CREATE TABLE IF NOT EXISTS public.system_settings (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), category text NOT NULL CHECK (category IN ('rate_limit', 'security', 'crisis', 'monitoring', 'general')), setting_key text NOT NULL, setting_value jsonb NOT NULL, description text, is_public boolean DEFAULT false, requires_restart boolean DEFAULT false, version int DEFAULT 1, previous_value jsonb, created_by text, created_at timestamp with time zone DEFAULT now(), updated_at timestamp with time zone DEFAULT now(), UNIQUE (category, setting_key)); CREATE TABLE IF NOT EXISTS public.user_profiles (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), clerk_user_id text UNIQUE NOT NULL, email text, created_at timestamp with time zone DEFAULT now(), updated_at timestamp with time zone DEFAULT now(), primary_support text CHECK (primary_support IN ('adhd', 'autism', 'dyslexia', 'executiveDysfunction', 'workingMemory', 'sensoryProcessing')), communication_style text DEFAULT 'balanced' CHECK (communication_style IN ('literal', 'balanced', 'warm')), preferred_response_length text DEFAULT 'standard' CHECK (preferred_response_length IN ('tldr', 'standard', 'detailed')), timing_preference text DEFAULT 'gentle-awareness' CHECK (timing_preference IN ('no-pressure', 'gentle-awareness', 'structured')), daily_cost_budget numeric DEFAULT 10.00, daily_token_limit int DEFAULT 5000, crisis_support_enabled boolean DEFAULT true, memory_sharing_enabled boolean DEFAULT false, analytics_enabled boolean DEFAULT true, success_patterns jsonb DEFAULT ('[]'::jsonb), stress_triggers jsonb DEFAULT ('[]'::jsonb), recovery_strategies jsonb DEFAULT ('[]'::jsonb), energy_patterns jsonb DEFAULT ('{}'::jsonb), timezone text DEFAULT 'UTC', language text DEFAULT 'en', accessibility_preferences jsonb DEFAULT ('{}'::jsonb), energy_level text DEFAULT 'medium' CHECK (energy_level IN ('low', 'medium', 'high')), ui_state text DEFAULT 'explore' CHECK (ui_state IN ('calm', 'focus', 'explore', 'crisis', 'flow', 'social', 'planning', 'recovery')), task_breakdown_enabled boolean DEFAULT true, progress_celebration_enabled boolean DEFAULT true, current_stress_level int DEFAULT 3 CHECK (current_stress_level >= 1 AND current_stress_level <= 10), current_social_battery int DEFAULT 8 CHECK (current_social_battery >= 0 AND current_social_battery <= 10), burnout_risk_score int DEFAULT 0 CHECK (burnout_risk_score >= 0 AND burnout_risk_score <= 100), crisis_mode_active boolean DEFAULT false, last_crisis_event timestamp with time zone, onboarding_completed boolean DEFAULT false, onboarding_responses jsonb DEFAULT ('{}'::jsonb), first_name text, last_name text, current_org_id text, current_org_role text, current_org_name text); CREATE TABLE IF NOT EXISTS public.ai_providers (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), provider_id text UNIQUE NOT NULL, name text NOT NULL, provider_type text NOT NULL CHECK (provider_type IN ('openai', 'anthropic', 'azure', 'google', 'custom')), enabled boolean DEFAULT true, priority int DEFAULT 5 CHECK (priority >= 0 AND priority <= 10), model_name text, api_key_encrypted text, endpoint_url text, temperature numeric DEFAULT 0.7 CHECK (temperature >= 0 AND temperature <= 2), max_tokens int DEFAULT 4000, requests_per_minute int, tokens_per_minute int, last_health_check timestamp with time zone, health_status text DEFAULT 'unknown' CHECK (health_status IN ('healthy', 'unhealthy', 'unknown')), average_latency numeric, error_rate numeric DEFAULT 0, total_requests int DEFAULT 0, total_tokens int DEFAULT 0, total_cost_usd numeric DEFAULT 0.00, last_used timestamp with time zone, created_at timestamp with time zone DEFAULT now(), updated_at timestamp with time zone DEFAULT now()); CREATE TABLE IF NOT EXISTS public.system_analytics (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), date date NOT NULL, period_type text NOT NULL CHECK (period_type IN ('hour', 'day', 'week', 'month')), total_conversations int DEFAULT 0, active_users int DEFAULT 0, new_users int DEFAULT 0, returning_users int DEFAULT 0, crisis_interventions int DEFAULT 0, crisis_resolutions int DEFAULT 0, auto_detection_rate numeric DEFAULT 0, intervention_success_rate numeric DEFAULT 0, avg_response_time_ms numeric DEFAULT 0, uptime_percentage numeric DEFAULT 100, error_count int DEFAULT 0, provider_usage jsonb DEFAULT ('{}'::jsonb), provider_costs jsonb DEFAULT ('{}'::jsonb), recovery_sessions_started int DEFAULT 0, recovery_sessions_completed int DEFAULT 0, avg_recovery_effectiveness numeric DEFAULT 0, created_at timestamp with time zone DEFAULT now(), UNIQUE (date, period_type)); CREATE TABLE IF NOT EXISTS public.system_monitoring (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), "timestamp" timestamp with time zone DEFAULT now(), cpu_usage_percent numeric, memory_usage_percent numeric, disk_usage_percent numeric, database_connections_active int, database_connections_max int, avg_response_time_ms numeric, error_rate_percent numeric, requests_per_minute int, active_users int, active_crisis_sessions int DEFAULT 0, high_risk_users int DEFAULT 0, crisis_interventions_today int DEFAULT 0, provider_health jsonb DEFAULT ('{}'::jsonb), custom_metrics jsonb DEFAULT ('{}'::jsonb)); CREATE TABLE IF NOT EXISTS public.admin_action_logs (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), admin_user_id text NOT NULL, admin_email text, action_type text NOT NULL CHECK (action_type IN ('user_activate', 'user_deactivate', 'user_exit_crisis', 'provider_create', 'provider_update', 'provider_delete', 'provider_test', 'settings_update', 'system_restart', 'data_export', 'crisis_intervention_manual', 'emergency_broadcast')), resource_type text NOT NULL CHECK (resource_type IN ('user', 'provider', 'setting', 'system')), resource_id text, action_details jsonb DEFAULT ('{}'::jsonb), previous_state jsonb, new_state jsonb, success boolean NOT NULL, error_message text, ip_address inet, user_agent text, "timestamp" timestamp with time zone DEFAULT now()); CREATE TABLE IF NOT EXISTS public.recovery_activities (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), name text NOT NULL, type text NOT NULL CHECK (type IN ('sensory', 'physical', 'mental', 'creative', 'restorative')), description text, duration_minutes int NOT NULL, energy_restoration_rate int NOT NULL CHECK (energy_restoration_rate >= 1 AND energy_restoration_rate <= 10), difficulty_level text NOT NULL CHECK (difficulty_level IN ('minimal', 'low', 'medium')), requirements text[] DEFAULT '{}', tags text[] DEFAULT '{}', icon text, is_default boolean DEFAULT false, is_active boolean DEFAULT true, created_at timestamp with time zone DEFAULT now()); CREATE TABLE IF NOT EXISTS public.crisis_baselines (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid REFERENCES public.user_profiles (id) ON DELETE CASCADE UNIQUE, avg_clicks_per_minute numeric DEFAULT 5.0, avg_response_length int DEFAULT 25, avg_error_rate numeric DEFAULT 0.05, avg_task_switches int DEFAULT 2, typical_response_time_ms int DEFAULT 3000, avg_energy_level numeric DEFAULT 7.0, avg_stress_level numeric DEFAULT 3.0, avg_cognitive_load numeric DEFAULT 4.0, avg_social_battery numeric DEFAULT 8.0, avg_session_duration_minutes int DEFAULT 45, preferred_working_hours int[] DEFAULT '{9,10,11,12,13,14,15,16,17}', sample_size int DEFAULT 0, last_calculated timestamp with time zone DEFAULT now(), confidence_score numeric DEFAULT 0.5, created_at timestamp with time zone DEFAULT now(), updated_at timestamp with time zone DEFAULT now()); CREATE TABLE IF NOT EXISTS public.tasks (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid REFERENCES public.user_profiles (id) ON DELETE CASCADE, title text NOT NULL, description text, priority text NOT NULL CHECK (priority IN ('low', 'medium', 'high', 'urgent')), energy_cost int NOT NULL CHECK (energy_cost >= 1 AND energy_cost <= 10), estimated_time int NOT NULL, actual_time int, completed boolean DEFAULT false, category text NOT NULL, deadline timestamp with time zone, breakdown_level int DEFAULT 3 CHECK (breakdown_level >= 0 AND breakdown_level <= 8), cognitive_load int NOT NULL CHECK (cognitive_load >= 1 AND cognitive_load <= 10), sequencing boolean DEFAULT true, timing_support boolean DEFAULT true, hyperfocus_warning boolean DEFAULT false, transition_support boolean DEFAULT false, sensory_considerations text[] DEFAULT '{}', dopamine_boosts text[] DEFAULT '{}', created_at timestamp with time zone DEFAULT now(), updated_at timestamp with time zone DEFAULT now(), completed_at timestamp with time zone); CREATE TABLE IF NOT EXISTS public.task_templates (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid REFERENCES public.user_profiles (id) ON DELETE CASCADE, name text NOT NULL, title_template text NOT NULL, description_template text, category text NOT NULL, default_priority text DEFAULT 'medium', default_energy_cost int DEFAULT 3, default_estimated_time int DEFAULT 30, default_breakdown_level int DEFAULT 3, default_hyperfocus_warning boolean DEFAULT false, default_transition_support boolean DEFAULT false, default_sensory_considerations text[] DEFAULT '{}', times_used int DEFAULT 0, last_used timestamp with time zone, created_at timestamp with time zone DEFAULT now(), updated_at timestamp with time zone DEFAULT now(), UNIQUE (user_id, name)); CREATE TABLE IF NOT EXISTS public.memory_cards (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid REFERENCES public.user_profiles (id) ON DELETE CASCADE, trigger_phrase text NOT NULL, content text NOT NULL, context_info text, category text NOT NULL CHECK (category IN ('personal', 'work', 'health', 'projects', 'communications', 'learning', 'coping', 'achievements')), importance text DEFAULT 'medium' CHECK (importance IN ('low', 'medium', 'high', 'critical')), cognitive_type text DEFAULT 'fact' CHECK (cognitive_type IN ('fact', 'process', 'template', 'reminder', 'accomplishment', 'strategy')), energy_level_created text CHECK (energy_level_created IN ('low', 'medium', 'high', 'crisis')), ui_state_created text, accessibility_notes text, created_at timestamp with time zone DEFAULT now(), updated_at timestamp with time zone DEFAULT now(), last_accessed timestamp with time zone DEFAULT now(), access_count int DEFAULT 0, relevance_score numeric DEFAULT 1.0, surfacing_keywords text[] DEFAULT '{}', surfacing_contexts text[] DEFAULT '{}', surfacing_energy_states text[] DEFAULT '{}', surfacing_ui_states text[] DEFAULT '{}', daily_reminder boolean DEFAULT false, specific_times time[], day_of_week int[], related_card_ids uuid[] DEFAULT '{}', tags text[] DEFAULT '{}', search_vector tsvector); CREATE TABLE IF NOT EXISTS public.conversations (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid REFERENCES public.user_profiles (id) ON DELETE CASCADE, conversation_id text NOT NULL, user_message text NOT NULL, ai_response text NOT NULL, response_metadata jsonb DEFAULT ('{}'::jsonb), ui_state text, energy_level text, cognitive_load text, "timestamp" timestamp with time zone DEFAULT now(), updated_at timestamp with time zone DEFAULT now(), user_feedback int CHECK (user_feedback IN (-1, 0, 1)), was_helpful boolean, follow_up_needed boolean DEFAULT false, extracted_facts jsonb DEFAULT ('[]'::jsonb), extracted_learnings jsonb DEFAULT ('[]'::jsonb), extracted_preferences jsonb DEFAULT ('[]'::jsonb), extracted_strategies jsonb DEFAULT ('[]'::jsonb), archived boolean DEFAULT false, archive_date timestamp with time zone); CREATE TABLE IF NOT EXISTS public.planning_sessions (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid REFERENCES public.user_profiles (id) ON DELETE CASCADE, focus_area text NOT NULL, start_time timestamp with time zone NOT NULL DEFAULT now(), end_time timestamp with time zone, tasks_planned int DEFAULT 0, tasks_completed int DEFAULT 0, energy_used int DEFAULT 0, quality text CHECK (quality IN ('poor', 'good', 'excellent')), notes text, achievements jsonb DEFAULT ('[]'::jsonb), lessons_learned jsonb DEFAULT ('[]'::jsonb), created_at timestamp with time zone DEFAULT now(), updated_at timestamp with time zone DEFAULT now()); CREATE TABLE IF NOT EXISTS public.task_categories (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid REFERENCES public.user_profiles (id) ON DELETE CASCADE, name text NOT NULL, color text DEFAULT '#3B82F6', icon text DEFAULT 'folder', default_energy_cost int DEFAULT 3, default_time_estimate int DEFAULT 30, auto_breakdown boolean DEFAULT false, created_at timestamp with time zone DEFAULT now(), updated_at timestamp with time zone DEFAULT now(), UNIQUE (user_id, name)); CREATE TABLE IF NOT EXISTS public.energy_patterns (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid REFERENCES public.user_profiles (id) ON DELETE CASCADE, energy_level int NOT NULL CHECK (energy_level >= 1 AND energy_level <= 10), stress_level int NOT NULL CHECK (stress_level >= 1 AND stress_level <= 10), cognitive_load int NOT NULL CHECK (cognitive_load >= 1 AND cognitive_load <= 10), social_battery int NOT NULL CHECK (social_battery >= 1 AND social_battery <= 10), activity_type text NOT NULL, ui_state text, time_of_day int CHECK (time_of_day >= 0 AND time_of_day <= 23), day_of_week int CHECK (day_of_week >= 0 AND day_of_week <= 6), sleep_quality int CHECK (sleep_quality >= 1 AND sleep_quality <= 10), physical_comfort int CHECK (physical_comfort >= 1 AND physical_comfort <= 10), "timestamp" timestamp with time zone DEFAULT now()); CREATE TABLE IF NOT EXISTS public.behavioral_patterns (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid REFERENCES public.user_profiles (id) ON DELETE CASCADE, interaction_type text NOT NULL CHECK (interaction_type IN ('click', 'input', 'scroll', 'focus', 'blur', 'keypress', 'error')), "timestamp" timestamp with time zone DEFAULT now(), duration_ms int, intensity numeric, metadata jsonb DEFAULT ('{}'::jsonb), session_id text NOT NULL, created_at timestamp with time zone DEFAULT now()); CREATE TABLE IF NOT EXISTS public.recovery_plans (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid REFERENCES public.user_profiles (id) ON DELETE CASCADE, name text NOT NULL, description text, target_recovery_time_minutes int NOT NULL, activity_ids uuid[] DEFAULT '{}', break_intervals_minutes int DEFAULT 5, adaptive_difficulty boolean DEFAULT true, environmental_needs text[] DEFAULT '{}', emergency_protocols text[] DEFAULT '{}', times_used int DEFAULT 0, effectiveness_rating numeric DEFAULT 0, last_used timestamp with time zone, is_active boolean DEFAULT true, is_default boolean DEFAULT false, created_at timestamp with time zone DEFAULT now(), updated_at timestamp with time zone DEFAULT now()); CREATE TABLE IF NOT EXISTS public.user_activity_snapshots (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid REFERENCES public.user_profiles (id) ON DELETE CASCADE, snapshot_date timestamp with time zone DEFAULT now(), total_conversations int DEFAULT 0, conversations_last_7_days int DEFAULT 0, conversations_last_30_days int DEFAULT 0, last_active timestamp with time zone, current_energy_level int, current_stress_level int, in_crisis_mode boolean DEFAULT false, crisis_events_count int DEFAULT 0, recovery_sessions_count int DEFAULT 0, avg_recovery_effectiveness numeric DEFAULT 0, memory_cards_count int DEFAULT 0, memory_cards_accessed_last_week int DEFAULT 0, is_active boolean DEFAULT true, requires_admin_attention boolean DEFAULT false, attention_reason text, data_retention_expires timestamp with time zone); CREATE TABLE IF NOT EXISTS public.planning_preferences (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid REFERENCES public.user_profiles (id) ON DELETE CASCADE UNIQUE, max_tasks_per_day int DEFAULT 8, preferred_planning_time text DEFAULT 'morning', use_time_blocking boolean DEFAULT false, enable_energy_tracking boolean DEFAULT true, executive_function_level int DEFAULT 5 CHECK (executive_function_level >= 1 AND executive_function_level <= 10), auto_breakdown_threshold int DEFAULT 60, hyperfocus_mode boolean DEFAULT false, transition_time int DEFAULT 5, cognitive_load_warnings boolean DEFAULT true, pomodoro_enabled boolean DEFAULT false, pomodoro_work_duration int DEFAULT 25, pomodoro_break_duration int DEFAULT 5, deadline_warnings boolean DEFAULT true, energy_budget_alerts boolean DEFAULT true, daily_planning_reminder boolean DEFAULT true, daily_planning_time time DEFAULT '09:00', created_at timestamp with time zone DEFAULT now(), updated_at timestamp with time zone DEFAULT now()); CREATE TABLE IF NOT EXISTS public.social_interactions (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid REFERENCES public.user_profiles (id) ON DELETE CASCADE, interaction_type text NOT NULL CHECK (interaction_type IN ('meeting', 'chat', 'call', 'email', 'social_media', 'group', 'presentation', 'networking', 'other')), duration_minutes int, energy_cost int CHECK (energy_cost >= 1 AND energy_cost <= 10), energy_gained int CHECK (energy_gained >= 0 AND energy_gained <= 10), context text, people_count int DEFAULT 1, was_planned boolean DEFAULT false, started_at timestamp with time zone DEFAULT now(), ended_at timestamp with time zone, created_at timestamp with time zone DEFAULT now()); CREATE TABLE IF NOT EXISTS public.crisis_events (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid REFERENCES public.user_profiles (id) ON DELETE CASCADE, severity_level text NOT NULL CHECK (severity_level IN ('mild', 'moderate', 'severe', 'critical')), confidence_score numeric NOT NULL CHECK (confidence_score >= 0 AND confidence_score <= 100), escalation_risk numeric NOT NULL CHECK (escalation_risk >= 0 AND escalation_risk <= 100), primary_triggers text[] DEFAULT '{}', detected_indicators jsonb DEFAULT ('{}'::jsonb), recommended_interventions text[] DEFAULT '{}', time_to_intervention_minutes int, intervention_taken text, intervention_successful boolean, detection_timestamp timestamp with time zone DEFAULT now(), intervention_timestamp timestamp with time zone, resolution_timestamp timestamp with time zone, user_feedback text, follow_up_needed boolean DEFAULT false, follow_up_completed boolean DEFAULT false, detection_method text DEFAULT 'automatic', false_positive boolean DEFAULT false, created_at timestamp with time zone DEFAULT now()); CREATE TABLE IF NOT EXISTS public.crisis_support_logs (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid REFERENCES public.user_profiles (id) ON DELETE CASCADE, crisis_type text CHECK (crisis_type IN ('overwhelm', 'anxiety', 'meltdown', 'shutdown', 'general')), severity text CHECK (severity IN ('mild', 'moderate', 'severe')), trigger_context text, strategies_provided jsonb DEFAULT ('[]'::jsonb), resources_offered jsonb DEFAULT ('[]'::jsonb), user_feedback_immediate text, follow_up_needed boolean DEFAULT false, professional_referral_suggested boolean DEFAULT false, emergency_resources_provided boolean DEFAULT false, crisis_started timestamp with time zone DEFAULT now(), crisis_resolved timestamp with time zone, follow_up_date timestamp with time zone, anonymized boolean DEFAULT false); CREATE TABLE IF NOT EXISTS public.recovery_preferences (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid REFERENCES public.user_profiles (id) ON DELETE CASCADE UNIQUE, preferred_activity_types text[] DEFAULT '{}', max_session_duration_minutes int DEFAULT 60, adaptive_scheduling boolean DEFAULT true, gentle_reminders boolean DEFAULT true, progress_tracking boolean DEFAULT true, sensory_regulation boolean DEFAULT true, environmental_controls jsonb DEFAULT ('{
        "lighting": "normal",
        "soundscape": "silent", 
        "temperature": "neutral",
        "aromatherapy": false
    }'::jsonb), emergency_break_triggers jsonb DEFAULT ('{
        "stressThreshold": 8,
        "energyThreshold": 20,
        "burnoutRiskThreshold": 70
    }'::jsonb), created_at timestamp with time zone DEFAULT now(), updated_at timestamp with time zone DEFAULT now()); CREATE TABLE IF NOT EXISTS public.recovery_sessions (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid REFERENCES public.user_profiles (id) ON DELETE CASCADE, activity_id uuid REFERENCES public.recovery_activities (id), start_time timestamp with time zone DEFAULT now(), end_time timestamp with time zone, planned_duration_minutes int NOT NULL, actual_duration_minutes int, energy_before int CHECK (energy_before >= 1 AND energy_before <= 10), stress_before int CHECK (stress_before >= 1 AND stress_before <= 10), energy_after int CHECK (energy_after >= 1 AND energy_after <= 10), stress_after int CHECK (stress_after >= 1 AND stress_after <= 10), quality text CHECK (quality IN ('poor', 'good', 'excellent')), completion_rate int DEFAULT 100 CHECK (completion_rate >= 0 AND completion_rate <= 100), interruptions int DEFAULT 0, notes text, environmental_settings jsonb DEFAULT ('{}'::jsonb), was_scheduled boolean DEFAULT false, was_emergency boolean DEFAULT false, created_at timestamp with time zone DEFAULT now()); CREATE TABLE IF NOT EXISTS public.subtasks (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), task_id uuid REFERENCES public.tasks (id) ON DELETE CASCADE, title text NOT NULL, completed boolean DEFAULT false, estimated_time int NOT NULL, order_position int NOT NULL, created_at timestamp with time zone DEFAULT now(), updated_at timestamp with time zone DEFAULT now(), completed_at timestamp with time zone); CREATE TABLE IF NOT EXISTS public.social_battery (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid REFERENCES public.user_profiles (id) ON DELETE CASCADE, current_level int NOT NULL CHECK (current_level >= 0 AND current_level <= 100), max_capacity int NOT NULL DEFAULT 100 CHECK (max_capacity >= 1 AND max_capacity <= 100), drain_rate numeric DEFAULT 1.0, recharge_rate numeric DEFAULT 2.0, last_interaction_id uuid REFERENCES public.social_interactions (id), last_recovery_session_id uuid REFERENCES public.recovery_sessions (id), "timestamp" timestamp with time zone DEFAULT now(), UNIQUE (user_id, "timestamp")); CREATE OR REPLACE FUNCTION public.get_contextual_memories(user_clerk_id text, current_input text, ui_state_context text = 'explore', energy_level text = 'medium', limit_results int = 5) RETURNS TABLE (id uuid, trigger_phrase text, content text, category text, relevance_score numeric, surfacing_reason text) LANGUAGE plpgsql SECURITY DEFINER VOLATILE AS $$
DECLARE
    user_profile_id UUID;
    input_keywords TEXT[];
BEGIN
    SELECT up.id INTO user_profile_id
    FROM public.user_profiles up
    WHERE up.clerk_user_id = user_clerk_id;
    IF user_profile_id IS NULL THEN
        RETURN;
    END IF;
    SELECT array_agg(word) INTO input_keywords
    FROM (
        SELECT unnest(string_to_array(lower(current_input), ' ')) as word
        WHERE char_length(unnest(string_to_array(lower(current_input), ' '))) > 3
        LIMIT 10
    ) keywords;
    PERFORM set_config('app.current_clerk_user_id', user_clerk_id, true);
    RETURN QUERY
    SELECT 
        mc.id,
        mc.trigger_phrase,
        mc.content,
        mc.category,
        mc.relevance_score,
        CASE 
            WHEN mc.surfacing_keywords && input_keywords THEN 'Keyword match'
            WHEN ui_state_context = ANY(mc.surfacing_ui_states) THEN 'UI state relevance'
            WHEN energy_level = ANY(mc.surfacing_energy_states) THEN 'Energy level match'
            ELSE 'Context similarity'
        END as surfacing_reason
    FROM public.memory_cards mc
    WHERE mc.user_id = user_profile_id
    AND (
        mc.surfacing_keywords && input_keywords
        OR ui_state_context = ANY(mc.surfacing_ui_states)
        OR energy_level = ANY(mc.surfacing_energy_states)
        OR mc.search_vector @@ plainto_tsquery('english', current_input)
    )
    ORDER BY mc.relevance_score DESC, mc.last_accessed DESC
    LIMIT limit_results;
END;
$$; CREATE OR REPLACE FUNCTION current_clerk_org_id() RETURNS text LANGUAGE sql STABLE AS $$
  SELECT (auth.jwt()->'o'->>'id')::TEXT;
$$; CREATE OR REPLACE FUNCTION public.update_memory_card_search_vector() RETURNS trigger LANGUAGE plpgsql VOLATILE AS $$
begin
    NEW.search_vector := 
        setweight(to_tsvector('english', coalesce(NEW.trigger_phrase, '')), 'A') ||
        setweight(to_tsvector('english', coalesce(NEW.content, '')), 'B') ||
        setweight(to_tsvector('english', coalesce(array_to_string(NEW.tags, ' '), '')), 'C');
    return NEW;
end;
$$; CREATE OR REPLACE FUNCTION public.search_memory_cards(user_clerk_id text, search_query text = '', categories text[] = NULL, energy_state text = NULL, ui_state_filter text = NULL, limit_results int = 10) RETURNS TABLE (id uuid, trigger_phrase text, content text, category text, importance text, relevance_score numeric, last_accessed timestamp with time zone, access_count int, tags text[], rank real) LANGUAGE plpgsql SECURITY DEFINER VOLATILE AS $$
DECLARE
    user_profile_id UUID;
BEGIN
    SELECT up.id INTO user_profile_id
    FROM public.user_profiles up
    WHERE up.clerk_user_id = user_clerk_id;
    IF user_profile_id IS NULL THEN
        RETURN;
    END IF;
    PERFORM set_config('app.current_clerk_user_id', user_clerk_id, true);
    RETURN QUERY
    SELECT 
        mc.id,
        mc.trigger_phrase,
        mc.content,
        mc.category,
        mc.importance,
        mc.relevance_score,
        mc.last_accessed,
        mc.access_count,
        mc.tags,
        CASE 
            WHEN search_query = '' THEN mc.relevance_score::REAL
            ELSE ts_rank_cd(mc.search_vector, plainto_tsquery('english', search_query))
        END as rank
    FROM public.memory_cards mc
    WHERE mc.user_id = user_profile_id
    AND (categories IS NULL OR mc.category = ANY(categories))
    AND (energy_state IS NULL OR energy_state = ANY(mc.surfacing_energy_states))
    AND (ui_state_filter IS NULL OR ui_state_filter = ANY(mc.surfacing_ui_states))
    AND (
        search_query = '' 
        OR mc.search_vector @@ plainto_tsquery('english', search_query)
        OR mc.trigger_phrase ILIKE '%' || search_query || '%'
        OR mc.content ILIKE '%' || search_query || '%'
    )
    ORDER BY 
        CASE 
            WHEN search_query = '' THEN mc.relevance_score::REAL
            ELSE ts_rank_cd(mc.search_vector, plainto_tsquery('english', search_query))
        END DESC,
        mc.last_accessed DESC
    LIMIT limit_results;
END;
$$; CREATE OR REPLACE FUNCTION public.update_memory_card_relevance() RETURNS trigger LANGUAGE plpgsql VOLATILE AS $$
begin
    NEW.relevance_score := greatest(0.1, 
        least(2.0, 
            (ln(NEW.access_count + 1) * 0.4) +
            (case 
                when NEW.last_accessed > now() - interval '7 days' then 0.6
                when NEW.last_accessed > now() - interval '30 days' then 0.3
                else 0.1
            end) +
            (case NEW.importance
                when 'critical' then 0.5
                when 'high' then 0.3
                when 'medium' then 0.0
                else -0.2
            end)
        )
    );
    return NEW;
end;
$$; CREATE OR REPLACE FUNCTION current_clerk_user_id() RETURNS text LANGUAGE sql STABLE AS $$
  SELECT (auth.jwt()->>'sub')::TEXT;
$$; CREATE OR REPLACE FUNCTION public.update_updated_at_column() RETURNS trigger LANGUAGE plpgsql VOLATILE AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$; CREATE OR REPLACE FUNCTION public.update_user_baselines(user_uuid uuid) RETURNS void LANGUAGE plpgsql SECURITY DEFINER VOLATILE AS $$
declare
    behavior_stats record;
    energy_stats record;
begin
    select 
        count(case when interaction_type = 'click' then 1 end)::decimal / nullif(extract(epoch from max(timestamp) - min(timestamp)) / 60, 0) as clicks_per_minute,
        avg(case when interaction_type = 'input' and (metadata->>'characterCount')::integer > 0 then (metadata->>'characterCount')::integer end) as avg_input_length,
        count(case when interaction_type = 'error' then 1 end)::decimal / nullif(count(*), 0) as error_rate,
        avg(duration_ms) as avg_duration,
        count(*) as sample_size
    into behavior_stats
    from (
        select * from public.behavioral_patterns 
        where user_id = user_uuid 
        order by timestamp desc 
        limit 100
    ) recent_behaviors;
    select 
        avg(energy_level) as avg_energy,
        avg(stress_level) as avg_stress,
        avg(cognitive_load) as avg_cognitive_load,
        avg(social_battery) as avg_social_battery
    into energy_stats
    from (
        select * from public.energy_patterns 
        where user_id = user_uuid 
        order by timestamp desc 
        limit 50
    ) recent_energy;
    insert into public.crisis_baselines (
        user_id,
        avg_clicks_per_minute,
        avg_response_length,
        avg_error_rate,
        typical_response_time_ms,
        avg_energy_level,
        avg_stress_level,
        avg_cognitive_load,
        avg_social_battery,
        sample_size,
        confidence_score,
        updated_at
    ) values (
        user_uuid,
        coalesce(behavior_stats.clicks_per_minute, 5.0),
        coalesce(behavior_stats.avg_input_length::integer, 25),
        coalesce(behavior_stats.error_rate, 0.05),
        coalesce(behavior_stats.avg_duration::integer, 3000),
        coalesce(energy_stats.avg_energy, 7.0),
        coalesce(energy_stats.avg_stress, 3.0),
        coalesce(energy_stats.avg_cognitive_load, 4.0),
        coalesce(energy_stats.avg_social_battery, 8.0),
        coalesce(behavior_stats.sample_size, 0),
        case when behavior_stats.sample_size >= 50 then 0.8 else 0.3 end,
        now()
    ) on conflict (user_id) do update set
        avg_clicks_per_minute = excluded.avg_clicks_per_minute,
        avg_response_length = excluded.avg_response_length,
        avg_error_rate = excluded.avg_error_rate,
        typical_response_time_ms = excluded.typical_response_time_ms,
        avg_energy_level = excluded.avg_energy_level,
        avg_stress_level = excluded.avg_stress_level,
        avg_cognitive_load = excluded.avg_cognitive_load,
        avg_social_battery = excluded.avg_social_battery,
        sample_size = excluded.sample_size,
        confidence_score = excluded.confidence_score,
        updated_at = excluded.updated_at;
end;
$$; CREATE OR REPLACE FUNCTION public.get_user_profile_data(clerk_id text) RETURNS TABLE (id uuid, communication_style text, preferred_response_length text, timing_preference text, primary_support text, daily_cost_budget numeric, daily_token_limit int, accessibility_preferences jsonb, energy_patterns jsonb) LANGUAGE plpgsql SECURITY DEFINER VOLATILE AS $$
BEGIN
    PERFORM set_config('app.current_clerk_user_id', clerk_id, true);
    RETURN QUERY
    SELECT 
        up.id,
        up.communication_style,
        up.preferred_response_length,
        up.timing_preference,
        up.primary_support,
        up.daily_cost_budget,
        up.daily_token_limit,
        up.accessibility_preferences,
        up.energy_patterns
    FROM public.user_profiles up
    WHERE up.clerk_user_id = clerk_id;
END;
$$; CREATE OR REPLACE FUNCTION check_jwt_v2_compatibility() RETURNS TABLE (component text, jwt_v2_support boolean, status text) LANGUAGE plpgsql VOLATILE AS $$
BEGIN
  RETURN QUERY
  SELECT 
    'RLS Policies'::TEXT as component,
    EXISTS(
      SELECT 1 FROM pg_policies 
      WHERE definition ~ 'validate_jwt_version\(\)' OR 
            definition ~ 'current_clerk_user_id\(\)' OR
            definition ~ 'current_clerk_org_'
    ) as jwt_v2_support,
    CASE 
      WHEN EXISTS(SELECT 1 FROM pg_policies WHERE definition ~ 'auth\.jwt\(\)->>')
           AND EXISTS(SELECT 1 FROM pg_policies WHERE definition ~ 'validate_jwt_version\(\)')
      THEN 'JWT v2 READY'
      ELSE 'NEEDS JWT v2 UPGRADE'
    END as status;
END $$; CREATE OR REPLACE FUNCTION public.create_memory_card(user_clerk_id text, trigger_text text, content_text text, context_info text = '', category_type text = 'personal', importance_level text = 'medium', cognitive_type_val text = 'fact', energy_level_created text = 'medium', ui_state_created text = 'explore', surfacing_keywords text[] = '{}', surfacing_contexts text[] = '{}', surfacing_energy_states text[] = '{}', surfacing_ui_states text[] = '{}', tags_array text[] = '{}') RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER VOLATILE AS $$
DECLARE
    user_profile_id UUID;
    new_card_id UUID;
BEGIN
    SELECT up.id INTO user_profile_id
    FROM public.user_profiles up
    WHERE up.clerk_user_id = user_clerk_id;
    IF user_profile_id IS NULL THEN
        RAISE EXCEPTION 'User profile not found for clerk_id: %', user_clerk_id;
    END IF;
    PERFORM set_config('app.current_clerk_user_id', user_clerk_id, true);
    INSERT INTO public.memory_cards (
        user_id,
        trigger_phrase,
        content,
        context_info,
        category,
        importance,
        cognitive_type,
        energy_level_created,
        ui_state_created,
        surfacing_keywords,
        surfacing_contexts,
        surfacing_energy_states,
        surfacing_ui_states,
        tags,
        access_count,
        relevance_score
    ) VALUES (
        user_profile_id,
        trigger_text,
        content_text,
        context_info,
        category_type,
        importance_level,
        cognitive_type_val,
        energy_level_created,
        ui_state_created,
        surfacing_keywords,
        surfacing_contexts,
        surfacing_energy_states,
        surfacing_ui_states,
        tags_array,
        0,
        1.0
    ) RETURNING id INTO new_card_id;
    RETURN new_card_id;
END;
$$; CREATE OR REPLACE FUNCTION public.cleanup_expired_memory_cards() RETURNS int LANGUAGE plpgsql SECURITY DEFINER VOLATILE AS $$
declare
    deleted_count integer;
begin
    delete from public.memory_cards 
    where importance = 'low'
    and access_count < 3
    and last_accessed < now() - interval '1 year'
    and created_at < now() - interval '1 year';
    get diagnostics deleted_count = row_count;
    return deleted_count;
end;
$$; CREATE OR REPLACE FUNCTION public.record_system_metrics() RETURNS void LANGUAGE plpgsql SECURITY DEFINER VOLATILE AS $$
begin
    insert into public.system_monitoring (
        cpu_usage_percent,
        memory_usage_percent,
        database_connections_active,
        database_connections_max,
        avg_response_time_ms,
        error_rate_percent,
        requests_per_minute,
        active_users,
        active_crisis_sessions,
        high_risk_users,
        crisis_interventions_today
    ) select
        45.0, -- CPU usage (placeholder)
        62.0, -- Memory usage (placeholder)
        15, -- DB connections active
        50, -- DB connections max
        145.0, -- Avg response time
        0.01, -- Error rate
        120, -- Requests per minute
        (select count(*) from public.user_profiles where updated_at >= now() - interval '1 hour'), -- Active users
        (select count(*) from public.crisis_events where resolution_timestamp is null), -- Active crisis sessions
        (select count(*) from public.crisis_events where severity_level in ('severe', 'critical') and detection_timestamp >= now() - interval '1 day'), -- High risk users
        (select count(*) from public.crisis_events where detection_timestamp >= current_date) -- Crisis interventions today
    ;
end;
$$; CREATE OR REPLACE FUNCTION public.get_or_create_user_profile(clerk_id text) RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER VOLATILE AS $$
declare
    user_id uuid;
begin
    select id into user_id 
    from public.user_profiles 
    where clerk_user_id = clerk_id;
    if user_id is null then
        insert into public.user_profiles (clerk_user_id)
        values (clerk_id)
        returning id into user_id;
    end if;
    return user_id;
end;
$$; CREATE OR REPLACE FUNCTION current_clerk_org_role() RETURNS text LANGUAGE sql STABLE AS $$
  SELECT (auth.jwt()->'o'->>'role')::TEXT;
$$; CREATE OR REPLACE FUNCTION public.update_memory_card_timestamp() RETURNS trigger LANGUAGE plpgsql VOLATILE AS $$
BEGIN
    NEW.last_accessed = NOW();
    RETURN NEW;
END;
$$; CREATE OR REPLACE FUNCTION check_task_completion() RETURNS trigger LANGUAGE plpgsql VOLATILE AS $$
begin
    if new.completed = true and old.completed = false then
        if (select count(*) from public.subtasks where task_id = new.task_id and completed = false) = 0 then
            update public.tasks 
            set completed = true, completed_at = now(), updated_at = now()
            where id = new.task_id and completed = false;
        end if;
    end if;
    return new;
end;
$$; CREATE OR REPLACE FUNCTION public.get_user_preferences(clerk_id text) RETURNS pg_catalog.json LANGUAGE plpgsql SECURITY DEFINER VOLATILE AS $$
DECLARE
    user_prefs json;
BEGIN
    SELECT json_build_object(
        'primarySupport', COALESCE(primary_support, 'adhd'),
        'communicationStyle', COALESCE(communication_style, 'balanced'),
        'responseLength', COALESCE(preferred_response_length, 'standard'),
        'energyLevel', COALESCE(energy_level, 'medium'),
        'uiState', COALESCE(ui_state, 'explore'),
        'reducedMotion', COALESCE((accessibility_preferences->>'reducedMotion')::boolean, false),
        'highContrast', COALESCE((accessibility_preferences->>'highContrast')::boolean, false),
        'textToSpeech', COALESCE((accessibility_preferences->>'textToSpeech')::boolean, false),
        'literalLanguageMode', COALESCE((accessibility_preferences->>'literalLanguageMode')::boolean, false),
        'taskBreakdownEnabled', COALESCE(task_breakdown_enabled, true),
        'memoryAidsEnabled', COALESCE(memory_sharing_enabled, true),
        'crisisDetectionEnabled', COALESCE(crisis_support_enabled, true),
        'progressCelebrationEnabled', COALESCE(progress_celebration_enabled, true),
        'currentStressLevel', COALESCE(current_stress_level, 3),
        'currentSocialBattery', COALESCE(current_social_battery, 8),
        'burnoutRiskScore', COALESCE(burnout_risk_score, 0),
        'crisisModeActive', COALESCE(crisis_mode_active, false),
        'costBudget', COALESCE(daily_cost_budget, 10.0),
        'dailyTokenLimit', COALESCE(daily_token_limit, 5000)
    )
    INTO user_prefs
    FROM public.user_profiles 
    WHERE clerk_user_id = clerk_id;
    RETURN COALESCE(user_prefs, json_build_object(
        'primarySupport', 'adhd',
        'communicationStyle', 'balanced',
        'responseLength', 'standard',
        'energyLevel', 'medium',
        'uiState', 'explore',
        'reducedMotion', false,
        'highContrast', false,
        'textToSpeech', false,
        'literalLanguageMode', false,
        'taskBreakdownEnabled', true,
        'memoryAidsEnabled', true,
        'crisisDetectionEnabled', true,
        'progressCelebrationEnabled', true,
        'currentStressLevel', 3,
        'currentSocialBattery', 8,
        'burnoutRiskScore', 0,
        'crisisModeActive', false,
        'costBudget', 10.0,
        'dailyTokenLimit', 5000
    ));
END;
$$; CREATE OR REPLACE FUNCTION public.access_memory_card(card_id uuid, user_clerk_id text) RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER VOLATILE AS $$
DECLARE
    user_profile_id UUID;
BEGIN
    SELECT up.id INTO user_profile_id
    FROM public.user_profiles up
    WHERE up.clerk_user_id = user_clerk_id;
    IF user_profile_id IS NULL THEN
        RETURN FALSE;
    END IF;
    PERFORM set_config('app.current_clerk_user_id', user_clerk_id, true);
    UPDATE public.memory_cards
    SET 
        access_count = access_count + 1,
        last_accessed = NOW()
    WHERE id = card_id 
    AND user_id = user_profile_id;
    RETURN FOUND;
END;
$$; CREATE OR REPLACE FUNCTION public.calculate_crisis_confidence(user_uuid uuid, time_window_minutes int = 30) RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER VOLATILE AS $$
declare
    baseline_data record;
    recent_patterns record;
    confidence_score decimal := 0;
    indicators jsonb := '{}';
begin
    select * into baseline_data from public.crisis_baselines where user_id = user_uuid;
    if baseline_data is null then
        return jsonb_build_object('confidence', 0, 'message', 'No baseline data available');
    end if;
    select 
        count(*) as total_interactions,
        count(case when interaction_type = 'click' then 1 end) as clicks,
        count(case when interaction_type = 'error' then 1 end) as errors,
        avg(duration_ms) as avg_duration
    into recent_patterns
    from public.behavioral_patterns 
    where user_id = user_uuid 
    and timestamp >= now() - (time_window_minutes || ' minutes')::interval;
    if recent_patterns.clicks > (baseline_data.avg_clicks_per_minute * time_window_minutes * 2) then
        confidence_score := confidence_score + 25;
        indicators := indicators || jsonb_build_object('rapid_clicking', true);
    end if;
    if recent_patterns.errors > (baseline_data.avg_error_rate * recent_patterns.total_interactions * 3) then
        confidence_score := confidence_score + 20;
        indicators := indicators || jsonb_build_object('error_spikes', true);
    end if;
    if recent_patterns.avg_duration > (baseline_data.typical_response_time_ms * 1.5) then
        confidence_score := confidence_score + 15;
        indicators := indicators || jsonb_build_object('delayed_responses', true);
    end if;
    confidence_score := least(confidence_score, 100);
    return jsonb_build_object(
        'confidence', confidence_score,
        'indicators', indicators,
        'sample_size', recent_patterns.total_interactions,
        'time_window_minutes', time_window_minutes
    );
end;
$$; CREATE OR REPLACE FUNCTION public.generate_daily_analytics(target_date date = current_date) RETURNS void LANGUAGE plpgsql SECURITY DEFINER VOLATILE AS $$
declare
    analytics_record record;
begin
    select
        coalesce(count(distinct c.user_id), 0) as active_users,
        coalesce(count(c.id), 0) as total_conversations,
        coalesce(count(distinct case when up.created_at::date = target_date then up.id end), 0) as new_users,
        coalesce(count(ce.id), 0) as crisis_interventions,
        coalesce(count(case when ce.intervention_successful = true then 1 end), 0) as crisis_resolutions,
        coalesce(count(rs.id), 0) as recovery_sessions_started,
        coalesce(count(case when rs.end_time is not null then 1 end), 0) as recovery_sessions_completed,
        coalesce(avg(case when rs.quality = 'excellent' then 3 when rs.quality = 'good' then 2 when rs.quality = 'poor' then 1 end), 0) as avg_recovery_effectiveness
    into analytics_record
    from public.conversations c
    full outer join public.user_profiles up on c.user_id = up.id
    full outer join public.crisis_events ce on c.user_id = ce.user_id and ce.detection_timestamp::date = target_date
    full outer join public.recovery_sessions rs on c.user_id = rs.user_id and rs.start_time::date = target_date
    where c.timestamp::date = target_date or up.created_at::date = target_date;
    insert into public.system_analytics (
        date,
        period_type,
        total_conversations,
        active_users,
        new_users,
        crisis_interventions,
        crisis_resolutions,
        recovery_sessions_started,
        recovery_sessions_completed,
        avg_recovery_effectiveness
    ) values (
        target_date,
        'day',
        analytics_record.total_conversations,
        analytics_record.active_users,
        analytics_record.new_users,
        analytics_record.crisis_interventions,
        analytics_record.crisis_resolutions,
        analytics_record.recovery_sessions_started,
        analytics_record.recovery_sessions_completed,
        analytics_record.avg_recovery_effectiveness
    ) on conflict (date, period_type) do update set
        total_conversations = excluded.total_conversations,
        active_users = excluded.active_users,
        new_users = excluded.new_users,
        crisis_interventions = excluded.crisis_interventions,
        crisis_resolutions = excluded.crisis_resolutions,
        recovery_sessions_started = excluded.recovery_sessions_started,
        recovery_sessions_completed = excluded.recovery_sessions_completed,
        avg_recovery_effectiveness = excluded.avg_recovery_effectiveness,
        updated_at = now();
end;
$$; CREATE OR REPLACE FUNCTION get_planning_analytics(user_uuid uuid, days_back int = 30) RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER STABLE AS $$
declare
    result jsonb;
    total_tasks integer;
    completed_tasks integer;
    completion_rate decimal;
    avg_energy_per_task decimal;
    avg_time_accuracy decimal;
    overdue_count integer;
begin
    select 
        count(*) filter (where created_at >= now() - interval '1 day' * days_back),
        count(*) filter (where completed = true and created_at >= now() - interval '1 day' * days_back),
        count(*) filter (where deadline < now() and completed = false)
    into total_tasks, completed_tasks, overdue_count
    from public.tasks 
    where user_id = user_uuid;
    completion_rate := case when total_tasks > 0 then (completed_tasks::decimal / total_tasks * 100) else 0 end;
    select coalesce(avg(energy_cost), 0)
    into avg_energy_per_task
    from public.tasks 
    where user_id = user_uuid and created_at >= now() - interval '1 day' * days_back;
    select coalesce(avg(case when estimated_time > 0 then (actual_time::decimal / estimated_time * 100) else null end), 0)
    into avg_time_accuracy
    from public.tasks 
    where user_id = user_uuid and actual_time is not null and created_at >= now() - interval '1 day' * days_back;
    result := jsonb_build_object(
        'totalTasks', total_tasks,
        'completedTasks', completed_tasks,
        'completionRate', completion_rate,
        'avgEnergyPerTask', avg_energy_per_task,
        'avgTimeAccuracy', avg_time_accuracy,
        'overdueCount', overdue_count,
        'generatedAt', now()
    );
    return result;
end;
$$;; CREATE OR REPLACE FUNCTION validate_jwt_version() RETURNS boolean LANGUAGE sql STABLE AS $$
  SELECT COALESCE((auth.jwt()->'v')::INTEGER >= 2, false);
$$; CREATE OR REPLACE FUNCTION public.archive_old_conversations() RETURNS int LANGUAGE plpgsql SECURITY DEFINER VOLATILE AS $$
declare
    archived_count integer;
begin
    update public.conversations 
    set archived = true, archive_date = now()
    where timestamp < now() - interval '90 days'
    and archived = false
    and (user_feedback is null or user_feedback != 1); -- Keep helpful conversations longer
    get diagnostics archived_count = row_count;
    return archived_count;
end;
$$; CREATE OR REPLACE FUNCTION public.log_admin_action(p_admin_user_id text, p_admin_email text, p_action_type text, p_resource_type text, p_resource_id text = NULL, p_action_details jsonb = '{}', p_success boolean = true, p_error_message text = NULL) RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER VOLATILE AS $$
declare
    log_id uuid;
begin
    insert into public.admin_action_logs (
        admin_user_id,
        admin_email,
        action_type,
        resource_type,
        resource_id,
        action_details,
        success,
        error_message
    ) values (
        p_admin_user_id,
        p_admin_email,
        p_action_type,
        p_resource_type,
        p_resource_id,
        p_action_details,
        p_success,
        p_error_message
    ) returning id into log_id;
    return log_id;
end;
$$; CREATE OR REPLACE FUNCTION public.set_session_user(clerk_user_id text) RETURNS void LANGUAGE plpgsql SECURITY DEFINER STABLE AS $$
BEGIN
    PERFORM set_config('app.current_clerk_user_id', clerk_user_id, true);
    IF NOT validate_jwt_version() THEN
        RAISE EXCEPTION 'JWT v2 or higher required';
    END IF;
END;
$$; CREATE TRIGGER update_conversations_updated_at BEFORE UPDATE ON public.conversations FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column(); CREATE TRIGGER update_subtasks_updated_at BEFORE UPDATE ON public.subtasks FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column(); CREATE TRIGGER update_user_profiles_updated_at BEFORE UPDATE ON public.user_profiles FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column(); CREATE TRIGGER update_memory_card_timestamp_trigger BEFORE UPDATE ON public.memory_cards FOR EACH ROW WHEN (old.access_count IS DISTINCT FROM new.access_count) EXECUTE FUNCTION public.update_memory_card_timestamp(); CREATE TRIGGER update_planning_sessions_updated_at BEFORE UPDATE ON public.planning_sessions FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column(); CREATE TRIGGER update_task_categories_updated_at BEFORE UPDATE ON public.task_categories FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column(); CREATE TRIGGER update_tasks_updated_at BEFORE UPDATE ON public.tasks FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column(); CREATE TRIGGER update_memory_relevance_trigger BEFORE UPDATE ON public.memory_cards FOR EACH ROW EXECUTE FUNCTION public.update_memory_card_relevance(); CREATE TRIGGER update_memory_search_vector_trigger BEFORE INSERT OR UPDATE ON public.memory_cards FOR EACH ROW EXECUTE FUNCTION public.update_memory_card_search_vector(); CREATE TRIGGER update_task_templates_updated_at BEFORE UPDATE ON public.task_templates FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column(); CREATE TRIGGER update_planning_preferences_updated_at BEFORE UPDATE ON public.planning_preferences FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column(); CREATE TRIGGER update_memory_cards_updated_at BEFORE UPDATE ON public.memory_cards FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column(); CREATE TRIGGER auto_complete_task_on_subtasks AFTER UPDATE ON public.subtasks FOR EACH ROW EXECUTE FUNCTION check_task_completion(); CREATE INDEX IF NOT EXISTS idx_system_analytics_date ON public.system_analytics USING btree (date); CREATE INDEX IF NOT EXISTS idx_recovery_sessions_start_time ON public.recovery_sessions USING btree (start_time); CREATE INDEX IF NOT EXISTS idx_recovery_activities_active ON public.recovery_activities USING btree (is_active); CREATE INDEX IF NOT EXISTS idx_social_battery_timestamp ON public.social_battery USING btree ("timestamp"); CREATE INDEX IF NOT EXISTS idx_user_activity_snapshots_user_id ON public.user_activity_snapshots USING btree (user_id); CREATE INDEX IF NOT EXISTS idx_social_battery_user_id ON public.social_battery USING btree (user_id); CREATE INDEX IF NOT EXISTS idx_crisis_logs_user_id ON public.crisis_support_logs USING btree (user_id); CREATE INDEX IF NOT EXISTS idx_user_profiles_energy_level ON public.user_profiles USING btree (energy_level); CREATE INDEX IF NOT EXISTS idx_social_interactions_interaction_type ON public.social_interactions USING btree (interaction_type); CREATE INDEX IF NOT EXISTS idx_user_profiles_crisis_mode ON public.user_profiles USING btree (crisis_mode_active) WHERE crisis_mode_active = true; CREATE INDEX IF NOT EXISTS idx_conversations_conversation_id ON public.conversations USING btree (conversation_id); CREATE INDEX IF NOT EXISTS idx_user_profiles_primary_support ON public.user_profiles USING btree (primary_support); CREATE INDEX IF NOT EXISTS idx_admin_action_logs_resource ON public.admin_action_logs USING btree (resource_type, resource_id); CREATE INDEX IF NOT EXISTS idx_energy_patterns_activity ON public.energy_patterns USING btree (activity_type); CREATE INDEX IF NOT EXISTS idx_system_analytics_period ON public.system_analytics USING btree (period_type); CREATE INDEX IF NOT EXISTS idx_admin_action_logs_admin ON public.admin_action_logs USING btree (admin_user_id); CREATE INDEX IF NOT EXISTS idx_memory_cards_user_ui_states ON public.memory_cards USING gin (surfacing_ui_states); CREATE INDEX IF NOT EXISTS idx_planning_sessions_user_id ON public.planning_sessions USING btree (user_id); CREATE INDEX IF NOT EXISTS idx_memory_cards_tags ON public.memory_cards USING gin (tags); CREATE INDEX IF NOT EXISTS idx_memory_cards_user_energy ON public.memory_cards USING gin (surfacing_energy_states); CREATE INDEX IF NOT EXISTS idx_user_profiles_stress_level ON public.user_profiles USING btree (current_stress_level); CREATE INDEX IF NOT EXISTS idx_subtasks_completed ON public.subtasks USING btree (completed); CREATE INDEX IF NOT EXISTS idx_user_activity_snapshots_attention ON public.user_activity_snapshots USING btree (requires_admin_attention) WHERE requires_admin_attention = true; CREATE INDEX IF NOT EXISTS idx_system_settings_category ON public.system_settings USING btree (category); CREATE INDEX IF NOT EXISTS idx_subtasks_order ON public.subtasks USING btree (task_id, order_position); CREATE INDEX IF NOT EXISTS idx_user_activity_snapshots_date ON public.user_activity_snapshots USING btree (snapshot_date); CREATE INDEX IF NOT EXISTS idx_crisis_events_detection_time ON public.crisis_events USING btree (detection_timestamp); CREATE INDEX IF NOT EXISTS idx_tasks_category ON public.tasks USING btree (category); CREATE INDEX IF NOT EXISTS idx_memory_cards_importance ON public.memory_cards USING btree (importance); CREATE INDEX IF NOT EXISTS idx_recovery_sessions_activity_id ON public.recovery_sessions USING btree (activity_id); CREATE INDEX IF NOT EXISTS idx_crisis_events_severity ON public.crisis_events USING btree (severity_level); CREATE INDEX IF NOT EXISTS idx_user_profiles_ui_state ON public.user_profiles USING btree (ui_state); CREATE INDEX IF NOT EXISTS idx_admin_action_logs_timestamp ON public.admin_action_logs USING btree ("timestamp"); CREATE INDEX IF NOT EXISTS idx_memory_cards_keywords ON public.memory_cards USING gin (surfacing_keywords); CREATE INDEX IF NOT EXISTS idx_recovery_plans_user_id ON public.recovery_plans USING btree (user_id); CREATE INDEX IF NOT EXISTS idx_recovery_activities_difficulty ON public.recovery_activities USING btree (difficulty_level); CREATE INDEX IF NOT EXISTS idx_behavioral_patterns_type ON public.behavioral_patterns USING btree (interaction_type); CREATE INDEX IF NOT EXISTS idx_crisis_events_user_id ON public.crisis_events USING btree (user_id); CREATE INDEX IF NOT EXISTS idx_behavioral_patterns_session ON public.behavioral_patterns USING btree (session_id); CREATE INDEX IF NOT EXISTS idx_system_settings_key ON public.system_settings USING btree (setting_key); CREATE INDEX IF NOT EXISTS idx_tasks_deadline ON public.tasks USING btree (deadline); CREATE INDEX IF NOT EXISTS idx_behavioral_patterns_user_id ON public.behavioral_patterns USING btree (user_id); CREATE INDEX IF NOT EXISTS idx_recovery_activities_type ON public.recovery_activities USING btree (type); CREATE INDEX IF NOT EXISTS idx_planning_sessions_active ON public.planning_sessions USING btree (user_id, end_time) WHERE end_time IS NULL; CREATE INDEX IF NOT EXISTS idx_tasks_user_id ON public.tasks USING btree (user_id); CREATE INDEX IF NOT EXISTS idx_user_profiles_clerk_id ON public.user_profiles USING btree (clerk_user_id); CREATE INDEX IF NOT EXISTS idx_tasks_completed ON public.tasks USING btree (completed); CREATE INDEX IF NOT EXISTS idx_subtasks_task_id ON public.subtasks USING btree (task_id); CREATE INDEX IF NOT EXISTS idx_memory_cards_updated_at ON public.memory_cards USING btree (updated_at DESC); CREATE INDEX IF NOT EXISTS idx_conversations_user_id ON public.conversations USING btree (user_id); CREATE INDEX IF NOT EXISTS idx_memory_cards_user_updated ON public.memory_cards USING btree (user_id, updated_at DESC); CREATE INDEX IF NOT EXISTS idx_crisis_logs_timestamp ON public.crisis_support_logs USING btree (crisis_started); CREATE INDEX IF NOT EXISTS idx_recovery_sessions_quality ON public.recovery_sessions USING btree (quality); CREATE INDEX IF NOT EXISTS idx_memory_cards_category ON public.memory_cards USING btree (category); CREATE INDEX IF NOT EXISTS idx_recovery_sessions_user_id ON public.recovery_sessions USING btree (user_id); CREATE INDEX IF NOT EXISTS idx_tasks_created_at ON public.tasks USING btree (created_at); CREATE INDEX IF NOT EXISTS idx_social_interactions_started_at ON public.social_interactions USING btree (started_at); CREATE INDEX IF NOT EXISTS idx_crisis_events_unresolved ON public.crisis_events USING btree (resolution_timestamp) WHERE resolution_timestamp IS NULL; CREATE INDEX IF NOT EXISTS idx_crisis_baselines_user_id ON public.crisis_baselines USING btree (user_id); CREATE INDEX IF NOT EXISTS idx_system_monitoring_recent ON public.system_monitoring USING btree ("timestamp" DESC); CREATE INDEX IF NOT EXISTS idx_planning_sessions_start_time ON public.planning_sessions USING btree (start_time); CREATE INDEX IF NOT EXISTS idx_recovery_plans_active ON public.recovery_plans USING btree (is_active); CREATE INDEX IF NOT EXISTS idx_task_templates_user_id ON public.task_templates USING btree (user_id); CREATE INDEX IF NOT EXISTS idx_memory_cards_user_id ON public.memory_cards USING btree (user_id); CREATE INDEX IF NOT EXISTS idx_user_profiles_onboarding_completed ON public.user_profiles USING btree (onboarding_completed); CREATE INDEX IF NOT EXISTS idx_admin_action_logs_action_type ON public.admin_action_logs USING btree (action_type); CREATE INDEX IF NOT EXISTS idx_user_activity_snapshots_active ON public.user_activity_snapshots USING btree (is_active); CREATE INDEX IF NOT EXISTS idx_memory_cards_search_vector ON public.memory_cards USING gin (search_vector); CREATE INDEX IF NOT EXISTS idx_tasks_priority ON public.tasks USING btree (priority); CREATE INDEX IF NOT EXISTS idx_social_interactions_user_id ON public.social_interactions USING btree (user_id); CREATE INDEX IF NOT EXISTS idx_user_profiles_burnout_risk ON public.user_profiles USING btree (burnout_risk_score) WHERE burnout_risk_score > 50; CREATE INDEX IF NOT EXISTS idx_conversations_timestamp ON public.conversations USING btree ("timestamp"); CREATE INDEX IF NOT EXISTS idx_behavioral_patterns_timestamp ON public.behavioral_patterns USING btree ("timestamp"); CREATE INDEX IF NOT EXISTS idx_energy_patterns_timestamp ON public.energy_patterns USING btree ("timestamp"); CREATE INDEX IF NOT EXISTS idx_system_settings_public ON public.system_settings USING btree (is_public) WHERE is_public = true; CREATE INDEX IF NOT EXISTS idx_task_categories_user_id ON public.task_categories USING btree (user_id); CREATE INDEX IF NOT EXISTS idx_usage_analytics_date ON public.usage_analytics USING btree (date); CREATE INDEX IF NOT EXISTS idx_recovery_preferences_user_id ON public.recovery_preferences USING btree (user_id); CREATE INDEX IF NOT EXISTS idx_system_monitoring_timestamp ON public.system_monitoring USING btree ("timestamp"); CREATE INDEX IF NOT EXISTS idx_energy_patterns_user_id ON public.energy_patterns USING btree (user_id); CREATE INDEX IF NOT EXISTS idx_memory_cards_user_contexts ON public.memory_cards USING gin (surfacing_contexts); CREATE INDEX IF NOT EXISTS idx_conversations_updated_at ON public.conversations USING btree (updated_at DESC); CREATE POLICY crisis_events_jwt_v2 ON public.crisis_events TO authenticated USING (user_id IN (SELECT id FROM public.user_profiles WHERE clerk_user_id = current_clerk_user_id()) AND validate_jwt_version()) ; CREATE POLICY subtasks_user_access_jwt_v2 ON public.subtasks TO authenticated USING (task_id IN (SELECT t.id FROM public.tasks t JOIN public.user_profiles up ON t.user_id = up.id WHERE up.clerk_user_id = current_clerk_user_id()) AND validate_jwt_version()) ; CREATE POLICY memory_cards_jwt_v2 ON public.memory_cards TO authenticated USING (user_id IN (SELECT id FROM public.user_profiles WHERE clerk_user_id = current_clerk_user_id()) AND validate_jwt_version()) ; CREATE POLICY public_settings_readable_jwt_v2 ON public.system_settings FOR SELECT TO authenticated USING (is_public = true AND validate_jwt_version()) ; CREATE POLICY recovery_activities_admin_update ON public.recovery_activities FOR UPDATE TO authenticated USING ((auth.jwt() ->> 'role') = 'service_role' AND validate_jwt_version()) WITH CHECK ((auth.jwt() ->> 'role') = 'service_role' AND validate_jwt_version()) ; CREATE POLICY task_categories_user_access_jwt_v2 ON public.task_categories TO authenticated USING (user_id IN (SELECT id FROM public.user_profiles WHERE clerk_user_id = current_clerk_user_id()) AND validate_jwt_version()) ; CREATE POLICY task_templates_user_access_jwt_v2 ON public.task_templates TO authenticated USING (user_id IN (SELECT id FROM public.user_profiles WHERE clerk_user_id = current_clerk_user_id()) AND validate_jwt_version()) ; CREATE POLICY planning_preferences_user_access_jwt_v2 ON public.planning_preferences TO authenticated USING (user_id IN (SELECT id FROM public.user_profiles WHERE clerk_user_id = current_clerk_user_id()) AND validate_jwt_version()) ; CREATE POLICY user_profiles_jwt_v2 ON public.user_profiles TO authenticated USING (clerk_user_id = current_clerk_user_id() AND validate_jwt_version()) ; CREATE POLICY energy_patterns_jwt_v2 ON public.energy_patterns TO authenticated USING (user_id IN (SELECT id FROM public.user_profiles WHERE clerk_user_id = current_clerk_user_id()) AND validate_jwt_version()) ; CREATE POLICY recovery_plans_jwt_v2 ON public.recovery_plans TO authenticated USING (user_id IN (SELECT id FROM public.user_profiles WHERE clerk_user_id = current_clerk_user_id()) AND validate_jwt_version()) ; CREATE POLICY social_interactions_jwt_v2 ON public.social_interactions TO authenticated USING (user_id IN (SELECT id FROM public.user_profiles WHERE clerk_user_id = current_clerk_user_id()) AND validate_jwt_version()) ; CREATE POLICY crisis_logs_jwt_v2 ON public.crisis_support_logs TO authenticated USING (user_id IN (SELECT id FROM public.user_profiles WHERE clerk_user_id = current_clerk_user_id()) AND validate_jwt_version()) ; CREATE POLICY planning_sessions_user_access_jwt_v2 ON public.planning_sessions TO authenticated USING (user_id IN (SELECT id FROM public.user_profiles WHERE clerk_user_id = current_clerk_user_id()) AND validate_jwt_version()) ; CREATE POLICY recovery_sessions_jwt_v2 ON public.recovery_sessions TO authenticated USING (user_id IN (SELECT id FROM public.user_profiles WHERE clerk_user_id = current_clerk_user_id()) AND validate_jwt_version()) ; CREATE POLICY admin_action_logs_jwt_v2 ON public.admin_action_logs TO authenticated USING ((current_clerk_org_role() IN ('admin', 'owner') OR (auth.jwt() ->> 'role') = 'service_role') AND validate_jwt_version()) ; CREATE POLICY recovery_activities_admin_delete ON public.recovery_activities FOR DELETE TO authenticated USING ((auth.jwt() ->> 'role') = 'service_role' AND validate_jwt_version()) ; CREATE POLICY recovery_preferences_jwt_v2 ON public.recovery_preferences TO authenticated USING (user_id IN (SELECT id FROM public.user_profiles WHERE clerk_user_id = current_clerk_user_id()) AND validate_jwt_version()) ; CREATE POLICY admin_monitoring_data_jwt_v2 ON public.system_monitoring TO authenticated USING ((current_clerk_org_role() IN ('admin', 'owner') OR (auth.jwt() ->> 'role') = 'service_role') AND validate_jwt_version()) ; CREATE POLICY recovery_activities_admin_modify ON public.recovery_activities FOR INSERT TO authenticated WITH CHECK ((auth.jwt() ->> 'role') = 'service_role' AND validate_jwt_version()) ; CREATE POLICY admin_user_snapshots_jwt_v2 ON public.user_activity_snapshots TO authenticated USING ((current_clerk_org_role() IN ('admin', 'owner') OR (auth.jwt() ->> 'role') = 'service_role') AND validate_jwt_version()) ; CREATE POLICY behavioral_patterns_jwt_v2 ON public.behavioral_patterns TO authenticated USING (user_id IN (SELECT id FROM public.user_profiles WHERE clerk_user_id = current_clerk_user_id()) AND validate_jwt_version()) ; CREATE POLICY ai_providers_service_only ON public.ai_providers TO authenticated USING ((auth.jwt() ->> 'role') = 'service_role' AND validate_jwt_version()) ; CREATE POLICY social_battery_jwt_v2 ON public.social_battery TO authenticated USING (user_id IN (SELECT id FROM public.user_profiles WHERE clerk_user_id = current_clerk_user_id()) AND validate_jwt_version()) ; CREATE POLICY usage_analytics_service_only ON public.usage_analytics TO authenticated USING ((auth.jwt() ->> 'role') = 'service_role' AND validate_jwt_version()) ; CREATE POLICY crisis_baselines_jwt_v2 ON public.crisis_baselines TO authenticated USING (user_id IN (SELECT id FROM public.user_profiles WHERE clerk_user_id = current_clerk_user_id()) AND validate_jwt_version()) ; CREATE POLICY conversations_jwt_v2 ON public.conversations TO authenticated USING (user_id IN (SELECT id FROM public.user_profiles WHERE clerk_user_id = current_clerk_user_id()) AND validate_jwt_version()) ; CREATE POLICY admin_system_settings_jwt_v2 ON public.system_settings TO authenticated USING ((current_clerk_org_role() IN ('admin', 'owner') OR (auth.jwt() ->> 'role') = 'service_role') AND validate_jwt_version()) ; CREATE POLICY tasks_user_access_jwt_v2 ON public.tasks TO authenticated USING (user_id IN (SELECT id FROM public.user_profiles WHERE clerk_user_id = current_clerk_user_id()) AND validate_jwt_version()) ; CREATE POLICY recovery_activities_read_all ON public.recovery_activities FOR SELECT TO authenticated USING (validate_jwt_version()) ; CREATE POLICY admin_analytics_jwt_v2 ON public.system_analytics TO authenticated USING ((current_clerk_org_role() IN ('admin', 'owner') OR (auth.jwt() ->> 'role') = 'service_role') AND validate_jwt_version()) 