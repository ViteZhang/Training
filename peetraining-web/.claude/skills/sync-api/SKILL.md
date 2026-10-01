---
description: 同步后端接口契约并重新生成客户端。只在用户输入 /sync-api 时使用。
disable-model-invocation: true
argument-hint: [契约标签，如 api-v0.3]
---

1. 把 API_SPEC_REF 改成 $0，执行 pnpm gen:api
2. 列出这次契约相对上一版新增、修改、删除的接口和字段
3. 跑 pnpm typecheck，列出因契约变化而报错的位置，不要自己修改业务代码，等我确认用哪张卡处理
