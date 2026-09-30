-- 070_neutral_region_ids.sql
-- Region ids become CapyDB's own (`eu-north-1`) instead of the hosting
-- provider's datacenter codes (`hel1`), which every public surface used to
-- carry verbatim (projects, ephemeral databases, /v1/regions, /status,
-- webhooks, CLI/MCP/SDK/Vercel). The catalog lives in
-- internal/model/regions.go; this migration moves the stored values onto it.
--
-- Stored, not translated at the boundary: region is emitted straight from rows
-- on many paths, and a translation layer leaks the datacenter code on every
-- path someone forgets to route through it.
--
-- hosts.datacenter keeps the provider location (operator-only, admin host
-- payloads). DEFAULT '' so inserts that name their columns explicitly keep
-- working.
--
-- Only the one known alias is rewritten. Any other value (a dev seed's 'dev',
-- a test fixture) is left exactly as it is rather than guessed at: failing the
-- migration would stop every control plane and worker from booting on such a
-- database (migrations auto-apply on boot), and a non-catalog region never
-- reaches the public region listing, which only names catalog regions.
-- Idempotent: re-running it is a no-op.
ALTER TABLE hosts ADD COLUMN IF NOT EXISTS datacenter TEXT NOT NULL DEFAULT '';

UPDATE hosts
   SET datacenter = region,
       region = 'eu-north-1'
 WHERE region = 'hel1';

UPDATE projects
   SET region = 'eu-north-1'
 WHERE region = 'hel1';
