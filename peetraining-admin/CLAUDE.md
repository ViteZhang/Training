# 考研Training 运营后台（peetraining-admin）· 项目上下文

考研Training 运营后台，独立仓库，只服务运营人员；App 在 `peetraining-web`，后端在 `peetraining-server`（Go）。
前端只通过后端 `api/openapi.yaml` 对接，不直接依赖后端代码。

## 依据文档（冲突时从上到下取）

1. `docs/prd.md`：PRD v3 后台章节（第 7 节逐页功能）
2. `docs/design/`：后台设计稿（`a7_*.dc.html`），`INDEX.md` 为页面编号 → 文件，`NOTES.md` 为调整说明
3. `docs/dev-spec.md`：研发规格
4. `docs/tech-plan.md`：部署与技术方案
5. `docs/open-questions.md`：未决问题

文档没写清的地方：先追加到 `docs/open-questions.md`，再停下来问，不自己发明业务规则或接口字段。

## 技术栈

- React 19 + Vite 8 + TypeScript + React Router 7 + TanStack Query + Ant Design 6
- 单包项目，无 pnpm workspace；共享代码内联在 `src/`
- `src/api-client/`：由 `openapi.yaml` 生成的类型（`generated/` 禁止手改）+ 统一请求处理
- `src/ui-tokens/`：颜色、字号、间距、圆角，取自设计稿

## 常用命令

```bash
pnpm install
pnpm dev                 # http://localhost:5173/admin/
pnpm typecheck
pnpm lint
pnpm test
pnpm build               # dist/，资源前缀 /admin/
pnpm gen:api             # 同步后端契约并重新生成类型
```

## 必须遵守

1. 接口只经 `src/api-client` 调用，不手写请求类型；契约缺字段就记到 `docs/open-questions.md` 并停下来
2. 权限只影响界面，安全由后端按 `x-roles` 保证
3. 后台页面带「账号名 + 时间」水印；手机号默认脱敏
4. 颜色与字号只从 `src/ui-tokens` 取，不写死
5. 掌握度、预估分、今日计划、额度、会员不在前端计算，只展示服务端结果
6. 页面按设计稿编号组织，文件头注释写明编号，如「// 7.13 兑换码」
7. 新增依赖在提交说明里写明理由，只用主流、活跃维护的库

## 目录约定

- `src/pages/`：按设计稿页面编号 7.x
- `src/components/`：布局、页面容器、参数编辑器、项目选择器
- `src/lib/`：请求、会话、菜单、主题、脱敏
- 标识符英文，面向用户的文案简体中文

## 官网（site/）

官网和后台在同一个仓库、同一个镜像里，但互不依赖：官网是纯静态页面，不用 React、Ant Design 和 `src/` 里的任何代码。

- `site/pages/*.html`：页面正文，第一行注释是标题、描述和导航项；`site/layout.html`：公共的页头页脚
- `site/public/`：原样输出的静态文件（样式、脚本、字体、Logo、robots.txt）
- `site/site.config.json`：ICP 备案号、公安备案号、模型公示文字；没填的项页面上不显示
- `scripts/build-site.mjs`：把页面套进模板输出到 `dist-site/`，不依赖第三方包
- `pnpm site:dev`：构建并在 http://localhost:4173 预览；`pnpm site:build`：只构建
- 协议正文、客服企业微信、下载地址都在浏览器里调后端公开接口（`/agreements/{kind}`、`/purchase-entry`、`/bootstrap`），和 App 用同一份后台配置，页面里不另存一份
- 颜色用 VI 品牌色（夜靛、琥珀、纸白），功能分类色与 App 设计稿一致；字体不加载外部资源
- 文案规矩同 App：不出现「押题」「预测卷」和提分保证；价格、额度写的是 PRD 13 节的初始值，后台改了要同步改 `site/pages/pricing.html` 和 `index.html`
- 新增页面后，要把路径加进 `deploy/nginx-admin.conf` 官网那段的正则，并更新宿主机 Nginx

## 部署

只有一个生产环境：后台 `https://training.dreamerlab.cn/admin/`，官网 `https://training.dreamerlab.cn/`。
Docker 多阶段构建（Node 22 + pnpm → Nginx），应用机 `docker compose pull && up -d`，
宿主机 dreamer-nginx 把 `/admin/` 和官网的页面路径反代到 `training-admin:80`，其余路径（`/api` 等）不变。
详见 `README.md` 与 `deploy/`。
