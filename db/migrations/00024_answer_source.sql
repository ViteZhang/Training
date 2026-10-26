-- +goose Up
-- PRD v3 1.7、1.7c、3.3b、4.7b、11.6、11.16（10 月 26 日更新）：导入题目缺答案的处理。
-- 答案分三档来源：来自你的资料（原文自带、从其他资料匹配、补充答案文件配对）/ 你已确认 / AI 参考答案（待确认）。
-- 档位由 questions.answer_origin 与 source 推出（rules.AnswerTier），这里补出处、匹配方式、AI 答案的确认与修改记录。
ALTER TABLE questions
  ADD COLUMN answer_match ENUM('original','other_material','answer_file') NULL COMMENT '来自你的资料的答案怎么来的：原文自带 / 从其他资料匹配 / 补充答案文件配对' AFTER answer_origin,
  ADD COLUMN answer_material_id BIGINT UNSIGNED NULL COMMENT '答案出处的资料；原文自带时与 source_material_id 相同' AFTER answer_match,
  ADD COLUMN answer_page INT UNSIGNED NULL AFTER answer_material_id,
  ADD COLUMN ai_answer_at DATETIME(3) NULL COMMENT '生成 AI 参考答案的时间（7.5 统计确认率与修改率）' AFTER answer_page,
  ADD COLUMN ai_answer_outcome ENUM('confirmed','edited') NULL COMMENT 'AI 参考答案被用户确认还是修改过' AFTER ai_answer_at,
  ADD COLUMN ai_answer_reports SMALLINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '客观题 AI 答案被报错次数，3 次自动撤回（PRD 11.16 第 7 条）' AFTER ai_answer_outcome;

-- 按 AI 参考答案判分的作答：失分归因、错题本、掌握度照常更新，但不计入预估分（PRD 11.6）。
ALTER TABLE attempts
  ADD COLUMN reference_only TINYINT(1) NOT NULL DEFAULT 0 COMMENT '按 AI 参考答案（待确认）判分，不计入预估分';

-- 1.7c 补充答案文件：answers 方式的导入任务只识别答案，配到缺答案的题上；target_job_id 是从哪次导入的确认页进来的。
ALTER TABLE import_jobs
  MODIFY COLUMN mode ENUM('question','reference','essay','answers') NOT NULL COMMENT '1.4 选的导入方式；answers = 1.7c 补充答案文件',
  ADD COLUMN target_job_id BIGINT UNSIGNED NULL COMMENT 'answers 任务：要补答案的导入任务（为空时补题库里的题）' AFTER mode;

ALTER TABLE import_answers
  ADD COLUMN stem TEXT NULL COMMENT '答案文件里带着的题干，题号对不上时按题干匹配' AFTER question_no;

CREATE TABLE answer_pairings (
  id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id    BIGINT UNSIGNED NOT NULL,
  job_id           BIGINT UNSIGNED NOT NULL COMMENT 'answers 方式的导入任务',
  target_kind      ENUM('item','question') NOT NULL COMMENT 'item = 还没入库的导入条目；question = 题库里的题',
  target_id        BIGINT UNSIGNED NOT NULL,
  import_answer_id BIGINT UNSIGNED NULL COMMENT '为空表示用户取消了这一条',
  method           ENUM('number','stem','manual') NOT NULL COMMENT '按年份 + 题号 / 按题干（请核对）/ 用户手动改配',
  status           ENUM('proposed','applied') NOT NULL DEFAULT 'proposed',
  created_at       DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at       DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_answer_pairings_target (job_id, target_kind, target_id),
  KEY idx_answer_pairings_owner (owner_user_id),
  CONSTRAINT fk_answer_pairings_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_answer_pairings_job FOREIGN KEY (job_id) REFERENCES import_jobs (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='1.7c 答案文件配对结果，用户确认后写进题目';

-- +goose Down
DROP TABLE answer_pairings;
ALTER TABLE import_answers DROP COLUMN stem;
ALTER TABLE import_jobs DROP COLUMN target_job_id, MODIFY COLUMN mode ENUM('question','reference','essay') NOT NULL;
ALTER TABLE attempts DROP COLUMN reference_only;
ALTER TABLE questions DROP COLUMN ai_answer_reports, DROP COLUMN ai_answer_outcome, DROP COLUMN ai_answer_at,
  DROP COLUMN answer_page, DROP COLUMN answer_material_id, DROP COLUMN answer_match;
