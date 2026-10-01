# peetraining-web

考研Training 的 App（Expo）与管理后台（React）。项目背景、规矩与文档优先级见 [CLAUDE.md](CLAUDE.md)；页面与规则见 docs/。

## 目录

```
apps/mobile          Expo SDK 57 + Expo Router（app/ 是路由，src/ 是组件与工具）
apps/admin           Vite + React + React Router + Ant Design，部署在 /admin/
packages/api-client  由 openapi.yaml 生成的类型（src/generated，禁止手改）+ 统一的请求处理
packages/ui-tokens   颜色、字号、间距、圆角，取自 VI v1.0
scripts/             gen-api（拉契约并生成客户端）、subset-fonts（标题字体子集）、gen-icons（App 图标）
```

## 本地开发

需要 Node 22+、pnpm 10。

```bash
pnpm install
pnpm gen:api                 # 同步接口契约（见下）
pnpm mock                    # 用契约起 mock 服务（:4010），后端没合并时先用它
pnpm --filter admin dev      # 管理后台 http://localhost:5173/admin/，/api 代理到 :8080（VITE_API_PROXY 可改）
pnpm typecheck && pnpm lint && pnpm test
```

### App

App 依赖原生模块（MMKV、SVG），需要 development build，不能用 Expo Go：

```bash
cd apps/mobile
API_BASE_URL=http://<本机局域网 IP>:4010 npx expo run:ios       # 需要 Xcode
API_BASE_URL=http://<本机局域网 IP>:4010 npx expo run:android   # 需要 Android Studio
```

环境变量（app.config.ts 读取）：`API_BASE_URL`（默认生产地址）、`APP_NAME`、`APP_ID`（默认 cn.dreamelab.training）、`APP_VARIANT`（development / preview / production）。

不打开模拟器也可以检查能否打包：`pnpm --filter mobile export`。

「我的」页底部的「基础组件演示」（仅非生产版）展示全部基础组件与五种状态。

## 接口契约

`pnpm gen:api` 拉取后端的 api/openapi.yaml 写入 packages/api-client/openapi.yaml，再生成类型：

- 开发暂存仓库里两个目录并排时，默认读 `../peetraining-server/api/openapi.yaml`，也可用 `API_SPEC_PATH` 指定
- 拆成两个云效仓库后：`API_SPEC_REPO=<后端仓库地址> API_SPEC_REF=api-v0.1 pnpm gen:api`

请求一律经 `@training/api-client`：自动带令牌、401 时刷新一次、写请求自动带 Idempotency-Key、错误统一转成 `ApiError`。

## 资源

- 字体：`python scripts/subset-fonts.py <NotoSerifSC_900Black.ttf> <NotoSerifSC_700Bold.ttf>`（ADR 0010）
- 图标：`python scripts/gen-icons.py`（按 VI 第 06 板；拿到 Logo 源文件后替换脚本里的 MARK 重新生成）
