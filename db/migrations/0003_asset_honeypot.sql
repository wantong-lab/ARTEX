-- 0003: 资产蜜罐静态签名评分（M1-04）。
-- 评分由 honeydetect 包在服务资产落库后 best-effort 写入；0 表示未命中/未评估。

ALTER TABLE assets ADD COLUMN IF NOT EXISTS honeypot_score DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE assets ADD COLUMN IF NOT EXISTS honeypot_evidence TEXT[] NOT NULL DEFAULT '{}';

CREATE INDEX IF NOT EXISTS idx_av2_honeypot ON assets(honeypot_score) WHERE honeypot_score > 0;