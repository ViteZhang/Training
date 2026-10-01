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

## 发布（T05）

版本号规则：`apps/mobile/app.config.ts` 的 `version` 用语义化版本（如 0.2.0），每周发版时手动加；
iOS buildNumber 与安卓 versionCode 由 EAS 生产配置自动递增。App 启动时把版本号传给 `GET /bootstrap`，
低于后台 7.8 配置的最低版本时强制更新（0.6b），有新版本时提示一次（0.6）。

| 配置 | 用途 | 命令 |
| --- | --- | --- |
| development | 开发用 development build（包名带 .development，可和正式版共存） | `pnpm --filter mobile build:dev` |
| preview | 内测：安卓 APK 上传 OSS，iOS 上传 TestFlight | `pnpm --filter mobile build:preview`，然后 `scripts/upload-apk.sh <apk> <版本号>`、`pnpm --filter mobile submit:testflight` |
| production | 正式上架（应用商店、App Store） | `pnpm --filter mobile build:prod` |

首次使用前：`npx eas-cli@latest login` 登录 Expo 账号，`cd apps/mobile && npx eas-cli@latest init` 生成 projectId；
iOS 需要 Apple 开发者账号（公司主体），在 eas.json 里填 `ascAppId`；EAS Update 在国内的下载速度见 open-questions Q08。

真机调试：iPhone 插线后 `npx expo run:ios --device`；安卓打开 USB 调试后 `npx expo run:android --device`；
或装 development 配置打出的包后 `pnpm --filter mobile start` 连接本机开发服务器。

管理后台：合并到 main 后由云效流水线执行 `scripts/deploy-admin.sh`（构建后同步到应用机 `/opt/training/admin`），
也可以本机执行 `scripts/deploy-admin.sh <应用机 SSH 地址>`。
