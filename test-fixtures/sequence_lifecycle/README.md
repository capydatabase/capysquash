# Sequences created and dropped

Sequences the history creates and later drops: directly with `DROP
SEQUENCE`, with the table or column they belong to (a `serial` or identity
column, or `OWNED BY`), and with `DROP IDENTITY`. Around them: a sequence
renamed and then altered, one dropped and created again under the same name,
and sequences detached with `OWNED BY NONE` that survive their table.

The baseline creates only the sequences that exist at the end, under their
final names, with the changes made to them; nothing (comments, privileges,
`ALTER SEQUENCE`) of a dropped sequence is kept.

`scripts/run-e2e.sh` squashes this history at every safety level and compares
the catalog of the result with that of the original history.
