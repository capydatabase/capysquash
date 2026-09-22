# Integration scripts

Helpers for adopting a squashed baseline in the migration tool that owns your
history. Both read `.squashmap.json` and the generated `000_baseline.sql` /
`010_data.sql` from the **working directory**, so copy the contents of the
`--output` directory into your project root (next to `prisma/` or `drizzle/`)
before running them. Both need `jq` and `DATABASE_URL`.

```bash
capysquash squash migrations/*.sql --output squashed/
cp squashed/.squashmap.json squashed/*.sql .
```

## `prisma-baseline.sh`

Applies the baseline and data operations, then marks the migrations listed
under `inputs` in `.squashmap.json` as applied with `prisma migrate resolve`,
so `prisma migrate deploy` stops trying to re-run them.

```bash
export DATABASE_URL='postgresql://user:password@localhost:5432/dbname'
./scripts/prisma-baseline.sh
```

Afterwards commit the squashed migrations, delete the old ones from
`prisma/migrations/`, and have teammates run `npx prisma migrate deploy`.

## `drizzle-reset.sh`

Backs up `drizzle/` (or `migrations/`), drops Drizzle's
`__drizzle_migrations` tracking table, applies the baseline and data
operations, and replaces the migration files with the squashed ones.

```bash
export DATABASE_URL='postgresql://user:password@localhost:5432/dbname'
./scripts/drizzle-reset.sh
```

This one is destructive: it drops migration tracking and replaces migration
files. Run it in development and coordinate with the team before touching a
shared database.
