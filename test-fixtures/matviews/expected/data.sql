-- Manual refresh operation (non-idempotent, separated to data file)
REFRESH MATERIALIZED VIEW CONCURRENTLY order_summary;
