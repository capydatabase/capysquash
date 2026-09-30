# Names PostgreSQL chooses

Indexes and constraints created without a name, where PostgreSQL appends a
number to the name it chooses because the plain one is taken: the same
unnamed index, foreign key, unique constraint and check twice, a check whose
first sibling is dropped, the constraints of a table and a domain created
under the name of one renamed before, one statement adding a check, a unique
constraint and a foreign key (PostgreSQL names them in that order), and a
check whose name another table takes explicitly and which the baseline
creates later. Unnamed indexes on two different tables are there too.

The baseline creates, groups and orders these statements differently, so it
spells out every name PostgreSQL chose that it could otherwise get
differently: numbered names, names another object of the schema takes, and
the names of indexes created without one.

`scripts/run-e2e.sh` squashes this history at every safety level and compares
the catalog of the result with that of the original history.
