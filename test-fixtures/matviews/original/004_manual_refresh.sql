-- Populate the view, which was created WITH NO DATA, then refresh it
-- concurrently: CONCURRENTLY needs a populated view and a unique index.
CREATE UNIQUE INDEX order_summary_category_idx ON order_summary (category);
REFRESH MATERIALIZED VIEW order_summary;
REFRESH MATERIALIZED VIEW CONCURRENTLY order_summary;
