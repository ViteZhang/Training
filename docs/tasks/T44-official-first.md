# T44–T49 官方题库先行（PRD v3 增补）

- 仓库：后端 + 前端（每张卡先后端、再前端）
- 依赖：T30、T36、T18、T07
- 计划完成：第 1 周开发全部页面
- 状态：进行中

## 依据

- 需求：《考研Training 官方题库需求文档（PRD v3 增补）》2026-11-02（下称「增补」），本仓库副本 docs/features/official-first.md
- 设计稿：「8-官方题库（新增）」页 n_*.dc.html，以及替换的 m1_subject（1.1）、m1_upload（1.5）；副本在 peetraining-web/docs/design/pages
- 本文未提到的规则沿用 PRD v3

## 拆卡（按依赖顺序）

| 卡 | 内容 | 页面 | 开关 |
| --- | --- | --- | --- |
| T44 | 院校专业课库 + 引导分流 + 官方首题 | 1.1、1.1a、1.1b、1.1c；后台「院校专业课库」 | onboarding_route、official_bank |
| T45 | 体验一道题 + 学科通用体验题 + AI 现场出题 | 1.9、1.9a、1.9b、1.10；后台 7.18 | trial、sample_trial、ai_live_question |
| T46 | 申请收录 | 1.11、1.11a；后台 7.16、7.1 卡片、侧栏红点、7.2 种子用户标记 | course_apply |
| T47 | 后台 AI 解析导入 | 7.17（含表格导入） | 后台不受开关影响 |
| T48 | 题库「官方 / 我的」与补充资料提示；上传页隐私承诺 | 3.1e、1.5 | 随 official_bank |

## 后端设计要点

### T44 院校专业课库与分流
- 新表 catalog_courses：院校全称、专业、专业课名称、代码（选填）、参考书目 JSON、考试年份、学科（哲学 / 文学…，给 1.10 与 7.16 筛选用）；
  官方题库状态不存，按 official_projects（院校 + 代码，没有代码按名称）实时算：published → 有，其他阶段 → 生产中，没有 → 无
- 新表 catalog_pending_courses：用户在 1.1 手动添加、库里没有的课，按「院校 + 代码或名称」归并计数；不记用户
- subjects 加 catalog_course_id、school、major（1.1 选课时写入）：用于「官方题库上线时通知在 1.1 选了该课的用户」
- App：GET /catalog/schools?q=、GET /catalog/courses?school=（按专业分组，含官方状态与官方题库摘要：版本、真题年份数、知识点数、模拟卷套数）
- 1.1b：POST /onboarding/first-question {bank_id}：确认已添加该官方题库，挑一道名词解释真题（按真题出现次数最多的知识点优先），建 practice_sessions(kind = official_first)；
  这个会话里的主观题批改不扣额度、不占体验次数（增补「体验与额度」）；每个账号只能建一次
- 后台（配置组）：院校专业课库增删改、待入库列表「入库 / 合并到已有课程」

### T45 体验
- 新表 trials（带 owner_user_id）：类型 own / sample / ai_live，题干、题型、分值、参考答案与来源（user / ai）、采分点、作答、批改结果、状态
- 次数：rule_params.trial.limit（默认 1），体验一道题与通用体验题共用，以批改成功为准，失败不计次；不扣任何额度
- POST /trials/recognize（粘贴文字或拍照 → 题型、分值；不是名词解释 / 简答 / 论述时提示更换）
- POST /trials（建体验，没有参考答案时 AI 生成并标「AI 参考答案」，提采分点，返回采分点个数）
- POST /trials/{id}/submit（批改，复用 practice 的批改能力与校验，不扣额度）
- POST /trials/{id}/save {action: import_all | only | discard, subject_id}：加入时题目、采分点、作答都保留，答案来源「AI 参考答案 · 待确认」或「来自你的资料」；不保存时全部删除
- 通用体验题：sample_directions（学科、方向）、sample_questions（参考教材、题型、分值、题干、答案、采分点、状态 待审核 / 已上线 / 已下线、来源 手工 / AI 批量 / AI 现场、缓存键、被体验次数）
- GET /sample-questions?discipline=；POST /sample-questions/live {subject_id}：未覆盖学科按专业课 + 参考教材现场生成一道，同一专业课缓存复用
- 后台 7.18：统计卡、学科方向树、题目列表与增改、AI 批量生成初稿、审核上线 / 下线、AI 出题缓存抽检

### T46 申请收录
- 新表 course_applications（owner_user_id、归并键、院校专业课、考试年份、入口），唯一键（用户, 归并键）：重复提交返回 409「你已申请过」
- 新表 course_application_groups：归并键（有代码按「院校 + 代码」，否则「院校 + 课程名」）、状态 待处理 / 建议立项 / 已联系 / 已立项 / 暂不收录、原因、最早待处理时间、对应立项
- 人数达阈值（rule_params.course_apply.suggest_threshold，默认 3）→ 建议立项 + 推企业微信群机器人（OPS_WECOM_WEBHOOK_URL；只发院校专业课、人数、后台链接）
- 状态变化通知所有申请人；官方题库首次发布时通知申请人和 1.1 选了该课的用户
- users 加 is_seed（7.2 运营标记）；7.16 申请人标记：付费（会员有效）、种子
- 后台 7.16 列表 / 详情 / 立项（生成 7.11 项目）/ 联系用户（站内信）/ 暂不收录（必填原因）/ 导出；7.1 概览「新的收录申请」

### T47 后台 AI 解析导入
- 新表 official_import_files（项目、类型 真题卷 / 参考书目录 / 授权讲义、OSS 键、状态、结果数）与 official_import_items（解析出的题目或知识点、置信度、原文位置、核对状态）
- 与用户端资料完全分表存放；解析能力复用 importer 的取文本与结构化
- 核对：接受 / 修改 / 驳回；接受的生成 official_drafts 并提交 7.13；采分点合计 ≠ 分值时标红且不能接受
- 表格导入：按增补「导入数据结构」字段的 xlsx / csv

### T48 题库与上传
- 知识点树、题目列表返回 official 与「我的」；板块标题带官方 / 我的题数
- 补充资料提示（规则写在 internal/rules，参数在 rule_params.supplement_prompt）：近 14 天某板块作答 ≥ 5、得分率 < 60%、官方题 < 5；
  或批改异议原因为「和我们老师讲的不一样」；或错题本 ≥ 30；一次一条；「以后再说」同板块 7 天不提示；注册当天不提示
- 1.5 隐私承诺四条、微信说明默认展开（前端）

## 前端（peetraining-web）

- 设计稿 n_*.dc.html 与新版 m1_subject、m1_upload 收进 docs/design/pages，INDEX.md 补编号
- App：新 1.1（开关关闭时回旧 1.1）、1.1a、1.1b、1.1c、1.9、1.9a、1.9b、1.10、1.11、1.11a、3.1e 改造、1.5 改造；1.2 目标分、1.3 备考安排移到首次批改之后（可跳过）
- 后台：侧栏「官方题库」分组改名与排序；7.16、7.17、7.18、院校专业课库；7.1「新的收录申请」；7.2 种子用户标记
- 埋点：onboarding_route、official_add、first_grade_done、trial_start / trial_submit、trial_save、apply_submit、supplement_prompt、upload_step、upload_exit_reason（只记行为和数量）

## 偏离增补的地方

- 「模型使用 DeepSeek」：按当前约定统一用 qwen3.8-flash（经 internal/ai），DeepSeek 接入渠道与数据条款待确认（open-questions）
- 待确认事项里的默认值按增补正文写入 rule_params，后台 7.8 可改

## 验收

- 新用户三条路都能在注册当天完成一次按采分点批改：有官方 → 首题；无官方 → 体验一道题；院校未定 → 通用体验题
- 体验与首题都不扣额度；体验次数用完后 1.1c 两个入口显示「已体验」
- 申请收录重复提交返回「你已申请过」；达到 3 人标「建议立项」；处理结果通知到所有申请人
- 7.17 解析的题核对后进入 7.13 审核，审核通过后能发布
- 所有用户内容接口有「拿别人的 ID 访问返回 404」的集成测试；关闭开关时接口返回 404
