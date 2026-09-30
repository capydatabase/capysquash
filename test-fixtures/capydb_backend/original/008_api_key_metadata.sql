ALTER TABLE api_keys
  ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'dashboard',
  ADD COLUMN IF NOT EXISTS device_name TEXT,
  ADD COLUMN IF NOT EXISTS created_by_user_id TEXT,
  ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

UPDATE api_keys
   SET source = CASE
     WHEN name = 'default' AND scopes = '["*"]'::jsonb THEN 'system'
     ELSE 'dashboard'
   END
 WHERE source IS NULL OR source = '';
