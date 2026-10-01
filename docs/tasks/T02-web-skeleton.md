# T02 前端工程骨架

- 仓库：前端（peetraining-web）
- 依赖：—
- 计划完成：10/5
- 状态：审查中

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
  - pnpm 工作区（node-linker=hoisted，Metro 需要扁平依赖）：apps/mobile、apps/admin、packages/api-client、packages/ui-tokens
  - mobile：Expo SDK 57（官方 blank-typescript 模板起步，依赖用 expo install 按 SDK 对齐）、Expo Router（app/ 下 (auth)、(onboarding)、(tabs) 分组，四个 Tab）、TanStack Query、Zustand、MMKV v4、react-native-svg；app.config.ts 从环境变量读显示名、包名、接口地址
  - 基础组件：Text（字号层级、最大放大 1.3 倍）、Button（主 / 次 / 文字 / 危险）、Card、Tag（含 AI 来源样式）、ProgressBar（琥珀进度 + 目标点）、BottomSheet、ConfirmDialog、Toast、Screen、Icon、Logo；五种状态 Skeleton / Loading（300 毫秒延迟）、EmptyState、ErrorState、AIGenerating（15 秒换安抚文案）/ AIFailed、QuotaSheet；演示页 app/dev/components.tsx
  - 0.1 启动页按 VI 竖版组合（夜靛底、琥珀「考研」、白色 Training）；原生启动图与 App 图标由 scripts/gen-icons.py 按 VI 第 06 板生成
  - 字体：Noto Serif SC 子集（GB2312 一级字）与 Sora 打包，useFonts 加载；ADR 0010
  - api-client：openapi-typescript 生成类型 + openapi-fetch；统一处理令牌、401 刷新一次（并发只刷一次）、写请求幂等键、ApiError；pnpm mock 用 Prism 按契约起 mock
  - admin：Vite + React Router + Ant Design（主色夜靛、系统字体、中文语言包）；登录壳（账号密码 → 短信两步验证）、按角色显示的五组侧边菜单（7.1–7.15）、「账号名 + 时间」水印、手机号脱敏工具
  - 依赖与理由：expo 系列（SDK 57 官方模块）、expo-router（路由，CLAUDE.md 选型）、@tanstack/react-query、zustand、react-native-mmkv + react-native-nitro-modules（MMKV v4 需要）、react-native-svg（Logo 与图标）、openapi-fetch / openapi-typescript（契约生成的类型安全请求）、@stoplight/prism-cli（契约 mock 服务）、antd / @ant-design/icons、react-router；测试 jest-expo、@testing-library/react-native、vitest、@testing-library/react；检查 eslint、typescript-eslint、eslint-plugin-react-hooks
- 偏离计划的地方与原因：
  - 标题字体子集用 GB2312 一级字 3755 字，而不是「常用 3500 字」：仓库里没有可靠的 3500 字表，一级字基本覆盖（ADR 0010）
  - 模拟器验收在开发环境里无法执行（没有 Xcode 与安卓模拟器），改为用 expo export 验证 Android 包能完整打包（Metro 打包成功、字体与图标都已收录）；真机与模拟器验证见下
  - Logo 源文件未到（Q07），图标与启动页用 VI 文件里的 SVG 构成
- 需要手动验证的步骤：
  - 本机 `cd apps/mobile && npx expo run:ios` 与 `npx expo run:android`，确认四个 Tab、启动页、颜色字体符合 VI
  - 在「我的 → 基础组件演示」里逐个点开底部弹层、确认弹窗、Toast、额度不足弹层
  - `pnpm --filter admin dev` 打开 http://localhost:5173/admin/，走一遍登录壳与菜单
- 验收结果：
  - [ ] mobile 在 iOS 与安卓模拟器启动——需要本机验证（Android 打包已在开发环境验证）
  - [x] admin dev 能打开登录壳与布局（vite build 通过，测试覆盖登录两步与菜单）
  - [x] 基础组件演示页，五种状态组件都能显示（组件测试覆盖）
  - [x] pnpm typecheck、lint、test 通过
