# Renames: a schema and the objects in it

A schema holding a table (with an index, a default and a comment), an enum, a
sequence, a function whose signature uses the enum, and a view is renamed;
later migrations add a column of the enum, a table with an index and a
foreign key to the first table, grants (one on the sequence) and a comment,
all under the new name.

The baseline creates the schema under its final name in its SCHEMAS section
and every object in it directly under that name: nothing is left to rename.

`scripts/run-e2e.sh` squashes this history at every safety level and compares
the catalog of the result, owners and privileges included, with that of the
original history.
