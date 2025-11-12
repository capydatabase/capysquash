-- Create base tables for materialized view
CREATE TABLE orders (
    id SERIAL PRIMARY KEY,
    customer_id INTEGER NOT NULL,
    product_id INTEGER NOT NULL,
    quantity INTEGER NOT NULL,
    total DECIMAL(10,2) NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE products (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    price DECIMAL(10,2) NOT NULL,
    category VARCHAR(100)
);

-- Create materialized view for order summaries
CREATE MATERIALIZED VIEW order_summary AS
SELECT
    p.category,
    COUNT(o.id) as total_orders,
    SUM(o.total) as total_revenue,
    AVG(o.quantity) as avg_quantity
FROM orders o
JOIN products p ON o.product_id = p.id
GROUP BY p.category
WITH NO DATA;

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
