# Training 前端（peetraining-web）· 项目上下文

考研Training：考研文科专业课的 AI 提分教练。考生把自己的专业课资料和题目导进来，AI 解析成个人题库，按采分点批改主观题、安排每日复习，并依据考生自己导入的真题估算专业课分数。
- 自建题库为主，不限院校；只做名词解释、简答、论述为主的文科专业课
- 底部导航四个 Tab：今日、题库、训练、我的
- App 显示名「考研Training」，包名 cn.dreamelab.training，都从配置读取，不写死

本仓库包含 App（Expo）与管理后台（React）。后端在 peetraining-server 仓库（Go），两边只通过它的 api/openapi.yaml 对接。

## 依据文档（冲突时从上到下取）

1. docs/prd.md：PRD v3。逐页功能与规则见第 6–10 节，通用状态见第 14 节
2. docs/design/：设计稿（App 94 页、后台 15 页）。INDEX.md 是页面编号 → 文件；NOTES.md 是每个模块的调整说明；pages/*.dc.html 是视觉稿源码
3. docs/design/vi/：视觉识别规范 v1.0（颜色、字体、字号、Logo、图标）；颜色与字体以 VI 为准，覆盖设计稿里的旧配色
4. docs/dev-spec.md：研发规格（第十一节前端规格、第十二节任务卡）
5. docs/tasks/：任务卡；docs/open-questions.md：未决问题

文档没写清的地方：先追加到 docs/open-questions.md，再停下来问；不要自己发明业务规则或接口字段。

## 技术栈

- pnpm 工作区
- apps/mobile：Expo SDK 57 + TypeScript + Expo Router；TanStack Query + Zustand；MMKV 本地存储；development build，不依赖 Expo Go
- apps/admin：React + Vite + TypeScript + React Router + TanStack Query + Ant Design
- packages/api-client：由 openapi.yaml 生成的类型与请求函数，禁止手改
- packages/ui-tokens：颜色、字号、间距、圆角，取自 docs/design/vi/

## 视觉规则（ui-tokens 的来源）

- 颜色：夜靛 #231F55（主按钮、选中、品牌）；琥珀 #FFB547（只用于预估分、进度、提分）；轨道紫 #3B377A；纸白 #FAF8F3（页面底，卡片仍为白）；墨 #1B1A17（正文）；灰 #6B675F（辅助文字）；线 #E4DFD4（描边、分隔）；绿 #2F9E6E（已掌握）；蓝 #3E6FD8（提示、链接、到期复习）；红 #D6453D（薄弱、错误、删除）
- 设计稿旧色替换：#1A1A1A → 夜靛（按钮）或墨（正文）；#6E6E6E → 灰；#EFEFEF → 线；#F2956B → 琥珀；#6FA8DC → 蓝；#C23B22 → 红
- 字号：H1 32/40 思源宋体 Heavy；H2 22/30 黑体 Bold；正文 16/24；辅助 13/18；分数 48/52 Sora Bold
- 字体：正文用系统中文字体（不打包）；标题打包 Noto Serif SC 常用 3500 字子集；数字与英文打包 Sora；不在运行时加载 Google Fonts
- 启动页与 App 图标按 VI 第 03、06 板

## 常用命令

- pnpm install
- pnpm gen:api：按 API_SPEC_REF 指定的标签，从 peetraining-server 拉取 openapi.yaml 并重新生成 packages/api-client
- pnpm mock：用 openapi.yaml 起 mock 服务，后端接口没合并时先用它开发
- pnpm --filter mobile start / android / ios
- pnpm --filter admin dev
- pnpm typecheck / lint / test

## 必须遵守

1. 一次只做一张任务卡；先输出实现计划等确认；不改卡片范围外的代码
2. 接口只经 packages/api-client 调用，不手写请求类型；契约里没有需要的字段，就记到 docs/open-questions.md 并停下来
3. 掌握度、预估分、今日计划、额度、会员不在前端计算，只展示服务端结果；唯一例外是客观题离线判分，联网后由服务端复核
4. 读 .dc.html 获取布局、文案和交互，用 ui-tokens 和基础组件重写，不照搬 HTML；颜色字体按上面的视觉规则替换
5. 基准尺寸 390×844；可点区域不小于 44×44；系统字体放大到 1.3 倍不破版
6. 每个页面处理五种状态：加载中（超过 300 毫秒才出骨架屏）、空、网络错误、AI 生成中、额度不足
7. 主观题、作文、整卷作答每 5 秒存草稿到 MMKV；写请求带 idempotency_key；模拟考试倒计时以服务端 deadline_at 为准
8. 功能开关从服务端读取；关闭的功能不显示入口
9. AI 生成的内容（答案、采分点、题目、解读、批改）在界面上按设计稿标注来源
10. 客户端不放任何密钥；不依赖 Google 服务（FCM、Google Fonts、Google 语音识别）
11. 文案用简体中文，以设计稿和 PRD 为准；对外文案不承诺分数
12. 管理后台的权限只影响界面，安全由后端保证；后台页面带「账号名 + 时间」水印；手机号默认脱敏
13. 新增依赖要在提交说明里写明理由，只用主流、活跃维护的库

## 目录约定

- apps/mobile/app/：路由。(auth) 模块 0；(onboarding) 模块 1；(tabs) 今日、题库、训练、我的；训练、整卷、背诵、作文等二级页面按模块分组
- apps/mobile/src/：components、features（按模块）、lib（请求、存储、埋点、功能开关）
- apps/admin/src/：pages（按页面编号 7.x）、components、lib
- 页面文件头部注释写明页面编号，如「// 4.7 批改结果」

## 环境与发布

- 只有一个生产环境：API 为 https://training.dreamelab.cn/api/v1，管理后台 https://training.dreamelab.cn/admin/
- 管理后台：云效流水线构建后同步到应用机 Nginx 的 /admin/ 目录
- App：EAS Build 三个配置 development / preview（内测）/ production；安卓内测包放 OSS，iOS 走 TestFlight

## 约定

- 标识符英文，面向用户的文案简体中文
- 分支 card/T12-import-flow；提交信息：T12: 导入流程页面

## 完成一张卡前自检

- [ ] pnpm typecheck、lint、test 通过
- [ ] 逐条对照卡片「验收」写出结果
- [ ] 对照 docs/design 检查每个页面与五种状态
- [ ] docs/tasks/README.md 的卡片状态已更新
- [ ] 列出需要在真机上手动验证的步骤
