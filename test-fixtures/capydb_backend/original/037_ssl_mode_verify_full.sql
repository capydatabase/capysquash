-- 037: canonical sslmode is verify-full, not require. The *.db.capydb.dev
-- certificate is publicly trusted, so verify-full validates chain + hostname
-- at no cost, and the docs and @capydb/drizzle already assume it; the worker
-- was still stamping 'require' at provision time. The worker literals now
-- write 'verify-full'; backfill the rows stamped with the old default so
-- reissued connection URLs pick it up.
UPDATE projects SET ssl_mode = 'verify-full' WHERE ssl_mode = 'require';
UPDATE preview_databases SET ssl_mode = 'verify-full' WHERE ssl_mode = 'require';
