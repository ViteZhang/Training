-- 教育学多子问：保存分问作答与分问批改结果。
ALTER TABLE attempts
  ADD COLUMN subanswers JSON NULL COMMENT '分问作答 [{subquestion_id,text}]';

ALTER TABLE gradings
  ADD COLUMN subquestion_results JSON NULL COMMENT '分问得分 [{subquestion_id,score,full_score,feedback}]';
