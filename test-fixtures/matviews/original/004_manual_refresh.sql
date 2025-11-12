-- Manual refresh operation (should be in separate file for non-idempotent operations)
REFRESH MATERIALIZED VIEW CONCURRENTLY order_summary;
