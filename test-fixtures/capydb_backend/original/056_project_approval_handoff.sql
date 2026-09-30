-- An approval minted by a person signed in to the dashboard can be handed to
-- another caller - the CLI, Terraform, CI - which presents it with its own
-- credential. Only those rows skip the minter binding; an approval minted by a
-- service principal stays bound to it. API keys cannot mint approvals at all
-- (see MintProjectApproval), so an agent holding an organization key cannot
-- authorize its own destructive action.
ALTER TABLE project_approvals ADD COLUMN IF NOT EXISTS handoff BOOLEAN NOT NULL DEFAULT false;
