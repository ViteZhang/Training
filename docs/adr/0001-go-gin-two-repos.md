# 0001 后端用 Go + Gin，前后端两个仓库

- 日期：2026-10-01
- 状态：已采纳

## 背景

后端要同时提供 App 与管理后台的 API，并运行资料解析、批改等后台任务；代码由 Claude Code 编写、技术合伙人接手维护。

## 决定

- 后端用 Go 1.27 + Gin；API 与 Worker 是同一套代码的两个入口（cmd/api、cmd/worker），打进同一个镜像
- 接口由 oapi-codegen 按 api/openapi.yaml 生成 Gin 接口与类型，处理器只写实现
- 前后端分为 peetraining-server、peetraining-web 两个云效仓库，只通过 openapi.yaml 对接（open-questions D9）
- 开发工具（oapi-codegen、golangci-lint）用 go.mod 的 tool 指令锁定版本，`go tool` 运行

## 放弃的方案与原因

- Node.js / NestJS 全栈：前后端同语言，但 CPU 密集的文本处理与并发任务不如 Go 稳，部署也要多一套运行时
- 单仓库：拆成两个仓库后，前后端的发布节奏与流水线互不影响，权限也能分开

## 影响

- 单二进制、静态编译，镜像小、部署简单
- 前端改接口必须先改后端契约并打标签
