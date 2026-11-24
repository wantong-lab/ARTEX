-- 0016: 订阅节点手动/自动选择。
ALTER TABLE egress_sources ADD COLUMN IF NOT EXISTS selected_node TEXT NOT NULL DEFAULT '';
ALTER TABLE egress_sources ADD COLUMN IF NOT EXISTS selection_mode TEXT NOT NULL DEFAULT 'auto';
