-- 0006: 语义记忆（M3-01）。pgvector 可用时 mem_entries 由 memory 包按配置维度建表
-- （ARTEX_EMBED_DIM，默认 1024）；本迁移只负责尝试安装扩展。
-- 扩展缺失时整个记忆层静默降级为关键词召回，主流程不受影响。

DO $$
BEGIN
    CREATE EXTENSION IF NOT EXISTS vector;
EXCEPTION WHEN OTHERS THEN
    RAISE NOTICE 'pgvector unavailable: memory falls back to keyword recall';
END
$$;