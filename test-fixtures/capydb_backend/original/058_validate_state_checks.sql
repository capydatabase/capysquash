-- Validate the state CHECK constraints that migration 022 added NOT VALID.
--
-- 022 skipped the scan so historical rows carrying pre-v2 vocabulary could not
-- fail the cutover. Every row in production has since been written by current
-- code (checked 2026-09-29: projects, preview_databases and jobs hold only
-- states the constraints allow), so the constraints can now cover existing rows
-- too. Until they are validated the planner cannot rely on them and a restored
-- or hand-edited row outside the enum would go unnoticed.
--
-- VALIDATE CONSTRAINT takes SHARE UPDATE EXCLUSIVE, which does not block reads
-- or writes, and is a no-op on a constraint that is already valid.
ALTER TABLE projects VALIDATE CONSTRAINT projects_state_check;
ALTER TABLE preview_databases VALIDATE CONSTRAINT preview_databases_state_check;
ALTER TABLE jobs VALIDATE CONSTRAINT jobs_state_check;
