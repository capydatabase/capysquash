ALTER TABLE clusters
  ADD COLUMN IF NOT EXISTS max_databases INTEGER NOT NULL DEFAULT 64;

UPDATE clusters
SET max_databases = 64
WHERE max_databases IS NULL OR max_databases <= 0;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'clusters_max_databases_check'
  ) THEN
    ALTER TABLE clusters
      ADD CONSTRAINT clusters_max_databases_check
      CHECK (max_databases > 0);
  END IF;
END $$;
