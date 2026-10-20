-- 249: 放宽模型能力测评作答明细的难度约束，与题库难度区间对齐。
-- 243 建表时题库只到 D5，约束写死为 difficulty BETWEEN 3 AND 5；v5.0.0 起新增
-- D6、v7.0.0 起新增 D7 题型，写入 d6-*/d7-* 作答行时触发 CHECK 违反，导致
-- ExecutePlan 在首个 D6 题中断、其余题按 0 分计入分母（能力分被严重拉低）。
-- 此处仅放宽上界，不改变既有数据。
ALTER TABLE model_benchmark_run_items
    DROP CONSTRAINT IF EXISTS model_benchmark_run_items_difficulty_check;
ALTER TABLE model_benchmark_run_items
    ADD CONSTRAINT model_benchmark_run_items_difficulty_check
    CHECK (difficulty BETWEEN 3 AND 7);
