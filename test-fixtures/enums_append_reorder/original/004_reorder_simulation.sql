-- This simulates a reorder scenario that should not be merged in conservative mode
-- The value 'banned' is being added in a position that breaks the append-only assumption
ALTER TYPE user_status ADD VALUE 'banned' AFTER 'suspended';
