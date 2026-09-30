DROP SEQUENCE temp_seq;

ALTER SEQUENCE ticket_numbers_old RENAME TO ticket_numbers;
ALTER SEQUENCE ticket_numbers INCREMENT BY 2;

DROP SEQUENCE recycled_seq;
CREATE SEQUENCE recycled_seq START 20;

DROP TABLE widgets;

ALTER TABLE gadgets DROP COLUMN legacy;
ALTER TABLE gadgets ALTER COLUMN counter DROP IDENTITY;
ALTER SEQUENCE gadget_serials OWNED BY NONE;

ALTER SEQUENCE survivor_seq OWNED BY NONE;
DROP TABLE doomed;
