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
