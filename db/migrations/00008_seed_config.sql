-- 初始配置：规则参数（PRD 第 11、13 节）、功能开关（PRD 3.4，默认关闭）、初试日期、通用作文评分标准。
-- 之后的修改走后台 7.8，不再改这个文件。

-- +goose Up
INSERT INTO rule_params (param_key, value, description) VALUES
('mastery', JSON_OBJECT(
  'self_assess', JSON_OBJECT('unknown', 0, 'vague', 30, 'mastered', 50),
  'objective_correct', JSON_OBJECT('easy', 10, 'medium', 15, 'hard', 20),
  'objective_wrong', -15,
  'subjective_slope', 25, 'subjective_offset', -10,
  'reveal_answer', -10,
  'recite', JSON_OBJECT('remembered', 8, 'vague', 2, 'forgot', -8),
  'overdue_daily', -3,
  'secondary_kp_factor', 0.5
), 'PRD 11.1 掌握分 M 的变化'),
('mastery_state', JSON_OBJECT(
  'mastered_min_m', 80, 'mastered_correct_days', 2, 'mastered_window_days', 30,
  'subjective_correct_rate', 0.8, 'weak_section_avg', 40,
  'false_mastery_window_days', 7, 'false_mastery_min_attempts', 2, 'false_mastery_max_rate', 0.5
), 'PRD 11.2 掌握状态、以为会了、短板'),
('review_interval', JSON_OBJECT(
  'reset_days', 1, 'vague_days', 2, 'steps', JSON_ARRAY(3, 7, 15, 30),
  'subjective_low_rate', 0.5, 'subjective_high_rate', 0.8
), 'PRD 11.3 复习间隔'),
('stage', JSON_OBJECT(
  'foundation_min_days', 150, 'strengthen_min_days', 60, 'sprint_min_days', 14,
  'early_sprint_max_days', 90, 'early_sprint_coverage', 0.7,
  'sprint_low_coverage', 0.6, 'sprint_new_kp_share', 0.1
), 'PRD 11.4 备考阶段（D16：≥150 基础、60–149 强化、14–59 冲刺、<14 考前）'),
('plan_mix', JSON_OBJECT(
  'foundation', JSON_OBJECT('new', 0.30, 'review', 0.30, 'weak', 0.20, 'recite', 0.20),
  'strengthen', JSON_OBJECT('new', 0.10, 'review', 0.25, 'weak', 0.45, 'recite', 0.20),
  'sprint',     JSON_OBJECT('new', 0.00, 'review', 0.30, 'weak', 0.50, 'recite', 0.20),
  'final',      JSON_OBJECT('new', 0.00, 'review', 0.35, 'weak', 0.20, 'recite', 0.45)
), 'PRD 11.5 今日计划构成'),
('plan', JSON_OBJECT(
  'minutes', JSON_OBJECT('objective', 1, 'term', 3, 'short_answer', 6, 'discussion', 12, 'recite', 1),
  'kp_exam_bonus', 0.5,
  'type_drill_per_week', 3, 'sprint_papers_per_week', 1, 'final_mock_every_days', 3, 'final_no_new_paper_days', 3
), 'PRD 11.5 单题用时与提分收益'),
('score_estimate', JSON_OBJECT(
  'measured_weight', 0.6, 'model_weight', 0.4,
  'measured_recent_papers', 2, 'qtype_recent_questions', 20, 'qtype_min_questions', 5,
  'width', JSON_OBJECT('1', 0.10, '2', 0.06, '3', 0.04)
), 'PRD 11.6 预估分'),
('loss_diagnosis', JSON_OBJECT('knowledge_max_m', 60, 'time_min_ratio', 0.3, 'tail_start_ratio', 0.67), 'PRD 11.7 失分诊断；tail_start_ratio 为「试卷后段」起点（按题序）'),
('wrong_book', JSON_OBJECT('remove_after_correct_days', 2, 'subjective_correct_rate', 0.8), 'PRD 11.8 错题本'),
('paper_time', JSON_OBJECT(
  'default_total_minutes', 180, 'check_minutes', 15,
  'per_question_minutes', JSON_OBJECT('term', 4, 'short_answer', 15, 'discussion', 40, 'objective', 2, 'essay', 60),
  'round_to_minutes', 5, 'overtime_ratio', 0.2, 'remind_left_minutes', 15, 'resume_window_minutes', 10
), 'PRD 11.9 整卷与模拟考试的时间'),
('ai_paper', JSON_OBJECT('exam_kp_share_min', 0.5, 'exam_kp_share_max', 0.7, 'targeted_weak_share', 0.5, 'weak_max_m', 60, 'targeted_avoid_days', 30), 'PRD 11.10 AI 组卷'),
('exam_profile', JSON_OBJECT('min_papers', 2, 'structure_recent_years', 3, 'high_freq_min_count', 2, 'missing_min_share', 0.1, 'missing_kp_ratio', 0.5, 'missing_max_mastery', 20), 'PRD 11.11 考情分析统计'),
('import_limits', JSON_OBJECT(
  'max_files', 10, 'max_pages_per_file', 200, 'max_mb_per_file', 50, 'max_images', 30, 'max_paste_chars', 20000,
  'chars_per_page', 1500, 'rows_per_page', 50
), 'PRD 11.12 导入规则'),
('answer_words', JSON_OBJECT('term', JSON_ARRAY(80, 150), 'short_answer', JSON_ARRAY(300, 500), 'discussion', JSON_ARRAY(800, NULL)), 'PRD 4.4 主观题建议字数'),
('essay', JSON_OBJECT('weekly_goal_default', 2, 'estimate_recent_essays', 3, 'repeat_tolerance', 6), 'PRD 11.13 作文评分'),
('grading', JSON_OBJECT('repeat_tolerance', 1, 'ai_question_offline_reports', 3), 'PRD 11.14、4.3'),
('quota', JSON_OBJECT(
  'free', JSON_OBJECT('subjects', 3, 'parse_pages_total', 100, 'import_questions_total', 500, 'grading_daily', 3, 'ai_questions_daily', 20, 'paper_grading_weekly', 1, 'essay_grading_weekly', 1),
  'member', JSON_OBJECT('subjects', 4, 'parse_pages_monthly', 1000, 'import_questions_total', NULL, 'grading_daily', NULL, 'ai_questions_daily', NULL, 'paper_grading_weekly', NULL, 'essay_grading_weekly', NULL)
), 'PRD 13.1 免费版与会员额度；NULL 表示不限（D12：免费版最多 3 门专业课）'),
('pricing', JSON_OBJECT(
  'sprint', JSON_OBJECT('name', '冲刺卡', 'cents', 5900, 'until', 'current_exam_end'),
  'season', JSON_OBJECT('name', '考季卡', 'cents', 16900, 'until', 'next_exam_end', 'recommended', TRUE),
  'monthly', JSON_OBJECT('name', '月卡', 'cents', 2990, 'days', 30)
), 'PRD 13.2 会员档位（测试价）'),
('growth', JSON_OBJECT('invite_days', 7, 'invite_max_days', 70, 'survey_days', 30), 'PRD 13.4 邀请与回访奖励'),
('account', JSON_OBJECT(
  'sms_code_ttl_seconds', 300, 'sms_resend_seconds', 60, 'sms_daily_limit', 10, 'sms_max_attempts', 5,
  'deletion_cooling_days', 7, 'access_token_minutes', 30, 'refresh_token_days', 60
), 'PRD 6 模块 0 验证码与账号'),
('retention', JSON_OBJECT('messages_days', 30, 'content_access_hours', 72, 'audit_log_days', 180, 'export_hours', 24), 'PRD 7.2.3、10.1、6.4 保留期限');

INSERT INTO feature_flags (flag_key, description, enabled_for_all) VALUES
('online_payment', '在线支付（6.5 购买、6.6、7.3 订单）', 0),
('official_bank',  '官方题库（3.1 添加入口、7.10–7.14）', 0),
('oral_recite',    '口述背诵（4.16）', 0),
('voice_answer',   '语音作答（4.4、4.20 语音转文字入口，D15）', 0),
('scanned_pdf',    '扫描版 PDF 导入（1.5）', 0),
('invite',         '邀请研友（6.8）', 0);

INSERT INTO exam_dates (exam_year, label, first_exam_start, first_exam_end, subject_exam_date) VALUES
(2027, '2027 研考', '2026-12-19', '2026-12-20', '2026-12-20');

INSERT INTO essay_rubrics (owner_user_id, subject_id, source, name, full_score, dimensions, is_active, origin) VALUES
(NULL, NULL, 'generic', '通用五维度标准', 150, JSON_ARRAY(
  JSON_OBJECT('name', '立意', 'description', '切题、观点明确、立意深刻', 'score', 30),
  JSON_OBJECT('name', '结构', 'description', '层次清晰、首尾呼应、过渡自然', 'score', 30),
  JSON_OBJECT('name', '内容与论证', 'description', '论据充实、论证有力', 'score', 30),
  JSON_OBJECT('name', '语言表达', 'description', '语言通顺、准确、得体', 'score', 30),
  JSON_OBJECT('name', '文采与亮点', 'description', '修辞、引用与独到见解', 'score', 30)
), 0, 'official');

-- +goose Down
DELETE FROM essay_rubrics WHERE source = 'generic' AND owner_user_id IS NULL;
DELETE FROM exam_dates WHERE exam_year = 2027;
DELETE FROM feature_flags;
DELETE FROM rule_params;
