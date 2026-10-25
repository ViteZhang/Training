-- +goose Up
-- 真题里的两类特殊题（2026-10-25 用户反馈）：
-- 1. 论述题按专业分组（「第一组：请选择文艺学专业的考生作答」，回答非报考专业不得分）：题目记上组名与适用专业，
--    专业课记上用户的报考方向，导入时只保留这一组，真题卷每个分组只按这一组计分。
-- 2. 多题共用一段材料：材料原文存在每道小题上（同一段材料存同一份，passage_key 相同），作答与批改时一起显示和使用。
-- 只加可空列，旧数据不受影响；MySQL 8 对 InnoDB 追加列是即时操作，不锁表重建。
ALTER TABLE questions
  ADD COLUMN passage      MEDIUMTEXT   NULL COMMENT '多题共用的阅读材料原文；同一段材料的题各存一份',
  ADD COLUMN passage_key  CHAR(16)     NULL COMMENT '共用材料的归组键（材料原文哈希前 16 位），同一段材料的题相同',
  ADD COLUMN group_label  VARCHAR(32)  NULL COMMENT '专业分组，如「第一组」',
  ADD COLUMN group_majors VARCHAR(255) NULL COMMENT '这一组适用的专业，如「文艺学、比较文学与世界文学」';

ALTER TABLE subjects
  ADD COLUMN my_majors VARCHAR(255) NULL COMMENT '报考方向：导入按专业分组的真题时默认只保留与它匹配的组';

-- +goose Down
ALTER TABLE subjects DROP COLUMN my_majors;
ALTER TABLE questions DROP COLUMN group_majors, DROP COLUMN group_label, DROP COLUMN passage_key, DROP COLUMN passage;
