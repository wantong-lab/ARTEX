-- 0019: 住宅网关生成节点数。
ALTER TABLE egress_sources ADD COLUMN IF NOT EXISTS gateway_count INTEGER NOT NULL DEFAULT 5;
