# Renames: enum, domain and composite types

An enum, a domain and a composite type are used by a table (default, CHECK
and column types) and by a function signature, with grants on the type and
the function. Then an enum value is renamed and another added next to it,
the three types are renamed, a composite attribute is renamed, and later
objects use the new names.

The baseline creates each type under its final name, with its final values
and attributes. The table's default and CHECK compare with the renamed value
under its new name, as PostgreSQL shows them; the domain's CHECK keeps the
name PostgreSQL derived from the domain's first name; the grants follow the
function to its new signature.

`scripts/run-e2e.sh` squashes this history at every safety level and compares
the catalog of the result, owners and privileges included, with that of the
original history.
