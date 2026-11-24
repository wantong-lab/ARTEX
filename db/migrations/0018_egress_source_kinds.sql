-- 0018: 允许 links/gateway 来源类型。
ALTER TABLE egress_sources DROP CONSTRAINT IF EXISTS egress_sources_kind_check;
ALTER TABLE egress_sources ADD CONSTRAINT egress_sources_kind_check CHECK (kind IN ('subscription','links','gateway'));
