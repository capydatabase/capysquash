# Schemas: DROP SCHEMA ... CASCADE, recreation, IF NOT EXISTS, AUTHORIZATION

A schema holding a table, an index, a view, an enum, a function, a comment
and a grant is dropped with CASCADE and created again with a different table
under the same name. A second schema is created with `IF NOT EXISTS` and
`AUTHORIZATION`, and created again with `IF NOT EXISTS` (a no-op). A third is
emptied and dropped without CASCADE.

The baseline's SCHEMAS section creates the schemas that exist at the end, as
the history created them; nothing the dropped schema held reaches the
baseline, and the recreated schema has none of the old one's privileges.

`scripts/run-e2e.sh` squashes this history at every safety level and compares
the catalog of the result, owners and privileges included, with that of the
original history.
