# T02 前端工程骨架

- 仓库：前端（peetraining-web）
- 依赖：—
- 计划完成：10/5
- 状态：未开始

## 目标

建立 App 与管理后台的工程、视觉基础和接口生成链路。

## 范围

- pnpm 工作区：apps/mobile、apps/admin、packages/api-client、packages/ui-tokens
- apps/mobile：Expo SDK 57 + Expo Router + TypeScript 严格模式；development build 配置；(auth)、(onboarding)、(tabs) 路由分组；四个 Tab（今日、题库、训练、我的）空页面；底部导航样式按设计稿
- packages/ui-tokens：按 CLAUDE.md「视觉规则」定义颜色、字号、间距、圆角；导出给 mobile 与 admin 使用
- 基础组件：按钮（主、次、文字）、卡片、标签、进度条、底部弹层、确认弹窗、Toast、骨架屏、空状态、错误状态、AI 生成中状态
- 字体打包：Noto Serif SC 常用字子集（标题）、Sora（数字）；正文用系统字体；写 ADR 0010
- App 图标与启动页按 VI 第 03、06 板（Logo 源文件未到时用 VI 文件里的 SVG）
- apps/admin：Vite + React Router + Ant Design；主色夜靛；登录页壳与带侧边菜单的布局（菜单分组按设计稿 7 模块）；水印组件
- pnpm gen:api 与 pnpm mock 脚本；API 地址、App 名称、包名从 app.config.ts 与环境变量读取
- TanStack Query、Zustand、MMKV 接好；请求层统一处理错误码、令牌刷新、idempotency_key

## 参考

- docs/design/vi/
- docs/design/pages/m2_home.dc.html（底部导航）
- docs/design/pages/a7_overview.dc.html（后台布局）
- docs/dev-spec.md 第十一节

## 验收

- pnpm --filter mobile ios 与 android 能在模拟器启动，看到四个 Tab，颜色字体符合 VI
- pnpm --filter admin dev 能打开后台登录壳与布局
- 基础组件有一个演示页，五种状态组件都能显示
- pnpm typecheck、lint、test 通过

## 不做

- 卡片范围以外的页面、接口和重构；发现需要的，记到 docs/open-questions.md

## 记录（开发中填写）

- 实现要点：
- 偏离计划的地方与原因：
- 需要手动验证的步骤：
