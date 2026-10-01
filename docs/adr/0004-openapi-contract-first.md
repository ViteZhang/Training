# 0004 OpenAPI 契约先行，前端按标签生成客户端

- 日期：2026-10-01
- 状态：已采纳

## 背景

前后端在两个仓库并行开发，App 发版后不能立即升级，接口必须稳定、可追溯。

## 决定

- api/openapi.yaml 是唯一的接口定义，后端仓库维护；改接口先改它，再 `make gen`
- 后端用 oapi-codegen 生成 Gin 接口与类型（internal/gen），处理器只写实现；契约里有、还没实现的接口由 stubgen 生成返回 501 的占位处理器（internal/http/stubs_gen.go），实现后自动消失
- 契约分批打标签（D17）：api-v0.1 覆盖模块 0、1、3 与导入、额度、功能开关；其余模块随对应卡增量加入，10/11 冻结 api-v1.0
- 前端按标签拉取 openapi.yaml 生成 packages/api-client；后端未完成时用契约起 mock 服务
- 流水线用 oasdiff 检查破坏性变更，有则必须升版本或兼容旧 App
- 测试里校验 openapi.yaml 是合法的 OpenAPI 3

## 放弃的方案与原因

- 代码先行、从代码生成文档：前端要等后端写完才能开工
- gRPC / Connect：App 与后台都要额外的客户端运行时，排查也不如 HTTP JSON 直观

## 影响

- 每次改契约都要打新标签并通知前端用哪个标签
- 枚举常量带类型前缀（HealthStatusOk），避免不同枚举同名值冲突
