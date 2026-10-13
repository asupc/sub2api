-- 规范化 schema 清单（用于恢复演练对比：库 A=恢复后 vs 库 B=官方全量）
-- 输出按 (kind, obj) 排序的扁平清单，两侧 diff 应为空
SELECT 'col' AS kind,
       table_name || ':' || column_name || '|' || data_type
       || COALESCE('(' || character_maximum_length || ')', '')
       || CASE is_nullable WHEN 'NO' THEN ' NOTNULL' ELSE '' END AS obj
FROM information_schema.columns WHERE table_schema = 'public'
UNION ALL
SELECT 'idx', tablename || ':' || indexname
FROM pg_indexes WHERE schemaname = 'public'
UNION ALL
SELECT 'con', conrelid::regclass::text || ':' || conname || ':' || contype::text
FROM pg_constraint WHERE connamespace = 'public'::regnamespace
UNION ALL
SELECT 'trg', event_object_table || ':' || trigger_name
FROM information_schema.triggers WHERE trigger_schema = 'public'
UNION ALL
SELECT 'fn', p.proname
FROM pg_proc p JOIN pg_namespace n ON p.pronamespace = n.oid
WHERE n.nspname = 'public' AND p.prokind = 'f'
ORDER BY 1, 2;
