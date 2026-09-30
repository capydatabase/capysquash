ALTER TABLE clusters
  ADD COLUMN IF NOT EXISTS backup_storage_mode TEXT NOT NULL DEFAULT 'local';

UPDATE clusters
SET backup_storage_mode = 'local'
WHERE backup_storage_mode IS NULL OR backup_storage_mode = '';

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'clusters_backup_storage_mode_check'
  ) THEN
    ALTER TABLE clusters
      ADD CONSTRAINT clusters_backup_storage_mode_check
      CHECK (backup_storage_mode IN ('local', 'walg'));
  END IF;
END $$;
