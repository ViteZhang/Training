-- T56 官方题库导入的「出处」：团队在本地用 AI 生成真题答案、知识点后从后台表格导入，出处要展示给用户（不管答案是不是 AI 生成的）。
-- 题目与知识点各加一列；官方内容和用户的副本都存。

-- +goose Up
ALTER TABLE questions ADD COLUMN reference_text VARCHAR(1000) NULL COMMENT '出处（T56）' AFTER analysis;
ALTER TABLE knowledge_points ADD COLUMN reference_text VARCHAR(1000) NULL COMMENT '出处（T56）' AFTER original_text;

-- +goose Down
ALTER TABLE knowledge_points DROP COLUMN reference_text;
ALTER TABLE questions DROP COLUMN reference_text;
