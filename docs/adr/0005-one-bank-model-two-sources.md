# 0005 一套题库模型、两种来源，学习数据只按「用户 + 知识点 / 题目」记

- 日期：2026-10-01
- 状态：已采纳

## 背景

产品以用户自建题库为主，官方题库作为可选的第二种来源（PRD 11.15）；两者要能同时练、同时批改，且不互相覆盖。

## 决定

- 题库 banks 记 source（user / official）与 owner_user_id（官方为空）；自建题库一门专业课一个
- 知识点、题目、采分点、试卷都挂在题库下，官方与自建共用同一套表
- 学习数据（kp_mastery、attempts、wrong_book、daily_plans）只按「用户 + 知识点 / 题目」记，规则代码不区分来源
- 用户添加官方题库记在 bank_subscriptions；同名知识点合并时在用户的知识点上记 official_kp_id，保留用户自己的表述和采分点
- 所有 AI 产物有 origin 字段（user_confirmed / ai_extracted / ai_generated / imported / official），界面据此标注来源
- 采分点只存当前版本；题目的 rubric_version 每次修改加 1，批改时把采分点快照写进 gradings.rubric_snapshot，历史批改不变（PRD 11.14）
- 删除资料的连带关系靠外键级联与 kp_sources：题目随资料删除（作答与错题级联删除）；知识点只有在没有其他来源时才删除；整卷成绩存在 paper_sessions，试卷删除时置空不删（PRD 11.12）

## 放弃的方案与原因

- 官方题库单独一套表：刷题、批改、复习的代码要写两遍
- 采分点按版本多行存储：查询复杂，而历史批改只需要快照

## 影响

- 官方题库发布新版本时，按 official_kp_id 对应关系迁移用户数据（T30）
