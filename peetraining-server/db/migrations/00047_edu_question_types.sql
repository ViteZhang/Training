-- 教育学 311/333：新增辨析、材料分析、分析论述三种规范题型。
-- ENUM 只在末尾追加，保留既有值的序号，不重写存量数据。
ALTER TABLE questions
  MODIFY COLUMN qtype ENUM(
    'single_choice','multi_choice','true_false','fill_blank','term','short_answer',
    'discussion','essay','calculation','other',
    'discrimination','material_analysis','analytical_discussion'
  ) NOT NULL COMMENT 'term 名词解释 / short_answer 简答 / discussion 论述 / essay 作文 / discrimination 辨析 / material_analysis 材料分析 / analytical_discussion 分析论述',
  ALGORITHM=INPLACE, LOCK=NONE;
