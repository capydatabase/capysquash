-- Create function to refresh order summary
CREATE OR REPLACE FUNCTION refresh_order_summary()
RETURNS TRIGGER AS $$
BEGIN
    REFRESH MATERIALIZED VIEW order_summary;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

-- Create trigger to refresh MV on order changes
CREATE TRIGGER refresh_order_summary_trigger
    AFTER INSERT OR UPDATE OR DELETE ON orders
    FOR EACH STATEMENT
    EXECUTE FUNCTION refresh_order_summary();
