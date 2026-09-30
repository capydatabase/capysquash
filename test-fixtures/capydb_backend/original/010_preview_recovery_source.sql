ALTER TABLE preview_databases
  ADD COLUMN IF NOT EXISTS source_kind TEXT,
  ADD COLUMN IF NOT EXISTS source_backup_key TEXT,
  ADD COLUMN IF NOT EXISTS source_restore_time TIMESTAMPTZ;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'preview_databases_source_kind_check'
  ) THEN
    ALTER TABLE preview_databases
      ADD CONSTRAINT preview_databases_source_kind_check
      CHECK (source_kind IS NULL OR source_kind IN ('backup', 'pitr'));
  END IF;
END $$;
