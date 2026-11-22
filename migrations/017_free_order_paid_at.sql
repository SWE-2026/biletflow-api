-- Free registrations were stored as paid orders without paid_at, which broke the analytics sales chart.
UPDATE orders SET paid_at = created_at WHERE status IN ('paid', 'refunded') AND paid_at IS NULL;
