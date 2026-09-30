# Sequences named by column defaults

Column defaults call `nextval` on sequences created after the tables that use
them, under names that sort before the tables' (`alpha_seq`, a sequence in
another schema, the sequence of another table's `serial` column), one through
`ALTER TABLE ... ADD COLUMN`; a later `ALTER SEQUENCE ... OWNED BY` ties a
sequence to one of those columns.

The baseline creates each sequence before the first table whose default names
it (the defaults are read for `nextval`, `currval`, `setval` and `::regclass`
references), and the `OWNED BY` after the table it names.

`scripts/run-e2e.sh` squashes this history at every safety level and compares
the catalog of the result with that of the original history.
