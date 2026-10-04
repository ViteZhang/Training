# 0003 后台任务用 Asynq + Redis，任务状态同时记在 MySQL

- 日期：2026-10-01
- 状态：已采纳

## 背景

资料解析、整卷批改、作文批改、导出、统计汇总都是耗时任务，需要排队、重试、定时（每天 0 点生成今日计划）。

## 决定

- 用 Asynq（Redis 作队列），Worker 是独立入口 cmd/worker
- 三个队列按权重消费：critical 6（用户在等的批改）、default 3、low 1（批量与统计）
- 定时任务用 Asynq Scheduler，时区 Asia/Shanghai；调度器全局只启一个，生产上只部署一个 worker 容器
- 规矩：任务必须可重复执行；入队在数据库事务提交之后；任务状态同时记在 MySQL（如 import_jobs），以 MySQL 为准展示给用户
- Redis 开 AOF；Redis 同时用于验证码与限频
- 优雅停机：停止调度，等进行中的任务在 SHUTDOWN_TIMEOUT 内完成，未完成的回到队列

## 放弃的方案与原因

- 阿里云消息队列：多一个付费服务和一套 SDK，内测规模用不上
- 只用 MySQL 轮询表做队列：重试、延迟、定时都要自己写

## 影响

- Redis 数据丢失时，进行中的任务以 MySQL 状态为准重新入队（各业务卡实现补偿）
- asynqmon 只在内网开放，用于排查队列
