-- A multi-column foreign key added later, its columns in another order
-- than the index's.
ALTER TABLE children ADD COLUMN parent_region text;
ALTER TABLE children ADD FOREIGN KEY (parent_code, parent_region)
    REFERENCES parents (code, region);

CREATE TABLE order_lines (
    id bigint PRIMARY KEY,
    sku text REFERENCES catalog.skus (sku)
);

-- Two tables that end up referencing each other, both through unique
-- indexes: their foreign keys are added once both tables exist.
CREATE TABLE teams (id bigint PRIMARY KEY, handle text NOT NULL, captain_handle text);
CREATE UNIQUE INDEX teams_handle_idx ON teams (handle);
CREATE TABLE players (
    id bigint PRIMARY KEY,
    handle text NOT NULL,
    team_handle text REFERENCES teams (handle)
);
CREATE UNIQUE INDEX players_handle_idx ON players (handle);
ALTER TABLE teams ADD FOREIGN KEY (captain_handle) REFERENCES players (handle);
