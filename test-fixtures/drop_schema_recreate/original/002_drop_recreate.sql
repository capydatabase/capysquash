DROP SCHEMA scratch CASCADE;
CREATE SCHEMA scratch;
CREATE TABLE scratch.work (id bigint PRIMARY KEY, body text NOT NULL);

CREATE SCHEMA IF NOT EXISTS reports;
CREATE TABLE reports.daily (day date PRIMARY KEY, total bigint NOT NULL DEFAULT 0);

DROP TABLE emptied.leftovers;
DROP SCHEMA emptied;
