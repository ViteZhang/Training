# peetraining-admin

考研Training 运营后台，独立于 App 仓库 `peetraining-web`。React + Vite + React Router + Ant Design，部署在 `https://training.dreamerlab.cn/admin/`。

## 目录

```
src/
├── api-client/          后端契约类型（generated/schema.ts 由 openapi.yaml 生成，禁止手改）+ 统一请求处理
├── ui-tokens/           设计 token（颜色、字号、间距），取自设计稿
├── pages/               页面（按设计稿编号 7.x）
├── components/          布局、页面容器等
├── lib/                 请求、会话、菜单、主题
└── main.tsx / App.tsx   入口与路由（basename=/admin）
deploy/
├── nginx.conf           容器内 Nginx（静态文件挂在 /admin/）
└── nginx-admin.conf     宿主机 dreamer-nginx 反代片段
docs/                    PRD、研发规格、设计稿、技术方案
Dockerfile               多阶段构建：Node 22 + pnpm → Nginx
docker-compose.yml       应用机部署（加入 dreamer-net，不映射宿主机端口）
scripts/gen-api.mjs      同步后端 openapi.yaml 并重新生成类型
```

## 本地开发

需要 Node 22.12+、pnpm 10。

```bash
pnpm install
pnpm dev                     # http://localhost:5173/admin/
VITE_API_PROXY=http://localhost:4010 pnpm dev   # 后端没起时用 mock
pnpm typecheck && pnpm lint && pnpm test
pnpm build                   # 产物在 dist/，资源前缀 /admin/
```

## 接口契约

接口统一经 `src/api-client` 调用，不手写请求类型。

```bash
pnpm gen:api                 # 默认读 ../peetraining-server/api/openapi.yaml
API_SPEC_PATH=/path/to/openapi.yaml pnpm gen:api
API_SPEC_REPO=<后端仓库地址> API_SPEC_REF=api-v0.1 pnpm gen:api
```

拉到的契约写入 `src/api-client/openapi.yaml`，类型生成到 `src/api-client/generated/schema.ts`，两者都提交。

## Docker 构建

本机需要 Docker（应用机或 CI 均可构建）：

```bash
docker build -t <ACR>/peetraining-admin:latest -t <ACR>/peetraining-admin:$(git rev-parse --short HEAD) .
```

镜像内 Nginx 监听 80，静态文件位于 `/admin/`，健康检查 `GET /healthz`。

### 基础镜像走 ACR

构建机访问 Docker Hub 超时时，先把 node/nginx 推到 ACR（在有网络的机器上执行一次）。
云效托管构建机是临时环境，不会继承本地或应用机上的 Docker 镜像；`--pull=false` 只在构建机本地已有该镜像时有效，所以基础镜像必须能从 ACR 或镜像加速器获取。

```bash
ACR=crpi-xxxxxxxxxxxx.cn-beijing.personal.cr.aliyuncs.com
NS=dreamer-lab

docker pull node:22-alpine
docker tag node:22-alpine "$ACR/$NS/node:22-alpine"
docker push "$ACR/$NS/node:22-alpine"

docker pull nginx:1.27-alpine
docker tag nginx:1.27-alpine "$ACR/$NS/nginx:1.27-alpine"
docker push "$ACR/$NS/nginx:1.27-alpine"
```

构建时指定基础镜像：

```bash
docker build \
  --build-arg NODE_IMAGE="$ACR/$NS/node:22-alpine" \
  --build-arg NGINX_IMAGE="$ACR/$NS/nginx:1.27-alpine" \
  -t "$ACR/$NS/peetraining-admin:latest" \
  -t "$ACR/$NS/peetraining-admin:$(git rev-parse --short HEAD)" .
```

## 首次部署

1. 构建并推送镜像到 ACR：

```bash
ACR=crpi-xxxxxxxxxxxx.cn-beijing.personal.cr.aliyuncs.com/<命名空间>
docker login "$ACR"
docker build -t "$ACR/peetraining-admin:latest" -t "$ACR/peetraining-admin:$(git rev-parse --short HEAD)" .
docker push "$ACR/peetraining-admin:latest"
docker push "$ACR/peetraining-admin:$(git rev-parse --short HEAD)"
```

2. 应用机准备目录与 `.env`：

```bash
mkdir -p /opt/dreamer/project/training-admin
cd /opt/dreamer/project/training-admin
# 放入本仓库的 docker-compose.yml，并按 .env.example 写 .env
# ADMIN_IMAGE=上面 ACR 地址 + 仓库名；ADMIN_TAG=latest
docker compose pull && docker compose up -d
docker compose ps            # 状态应为 healthy
```

3. 宿主机 Nginx 接管 `/admin/`：把 `deploy/nginx-admin.conf` 追加到
   `/opt/dreamer/nginx/conf.d/training.conf` 的 443 server 块内（`location /` 之前后均可，
   Nginx 按最长前缀匹配）。**如果现有配置里已有 `location /admin/` 指向 `/opt/training/admin`
   静态目录，先删除旧 location**，否则会和新反代冲突。然后：

```bash
docker exec dreamer-nginx nginx -t
docker exec dreamer-nginx nginx -s reload
```

4. 验证：`curl -I https://training.dreamerlab.cn/admin/` 返回 200/301，浏览器能登录。

## 日常发布与回滚

```bash
# 应用机
cd /opt/dreamer/project/training-admin
docker compose pull && docker compose up -d
docker compose logs --tail=50 admin
```

回滚：把 `.env` 的 `ADMIN_TAG` 改成上一个 git SHA（镜像仍在 ACR），再执行
`docker compose pull && docker compose up -d`。也可以临时覆盖：
`ADMIN_TAG=<上一个SHA> docker compose pull && ADMIN_TAG=<上一个SHA> docker compose up -d`。
下次正常发布前把 `ADMIN_TAG` 改回 `latest`。容器重建不影响 `/api`，后端无需重启。

## 云效流水线

后台是 Node 工程（React + Vite + pnpm），但运行产物是静态文件，最终跑在 Nginx 容器里。
推荐流水线：构建（可选测试）→ 构建并推送镜像 → 主机部署。

### 1. Node.js 构建（可选，建议保留）

- Node 版本选 22.x 最新补丁，不要用 20.10（Vite 8 需要 Node ≥22.12）
- 删除「配置 .npmrc 文件」步骤：项目没有私有 npm 依赖，不需要
- 执行命令：

```bash
npm install -g pnpm@10.28.0
pnpm install --frozen-lockfile
pnpm typecheck
pnpm lint
pnpm test
```

不要执行 `npm run build:prod`（没有这个脚本）；构建由 Dockerfile 完成。

### 2. 镜像构建并推送

- Dockerfile：`Dockerfile`
- 构建上下文：仓库根目录
- 镜像仓库：`<ACR>/<命名空间>/peetraining-admin`
- 标签：`latest` + 构建号或提交 SHA
- 构建参数（Docker Hub 拉取超时时必填）：
  `--build-arg NODE_IMAGE=<ACR>/<命名空间>/node:22-alpine`
  `--build-arg NGINX_IMAGE=<ACR>/<命名空间>/nginx:1.27-alpine`
- 认证用云效的 ACR 服务连接；不要在脚本里写密码

如果手工执行：

```bash
ACR=crpi-xxxxxxxxxxxx.cn-beijing.personal.cr.aliyuncs.com
IMAGE="$ACR/<命名空间>/peetraining-admin"
TAG=$(git rev-parse --short HEAD)
echo "$ACR_PASSWORD" | docker login --username="$ACR_USERNAME" --password-stdin "$ACR"
docker build -t "$IMAGE:latest" -t "$IMAGE:$TAG" .
docker push "$IMAGE:latest"
docker push "$IMAGE:$TAG"
```

`ACR_USERNAME`、`ACR_PASSWORD` 放流水线变量，`ACR_PASSWORD` 设为私密。

### 3. 主机部署

云效「主机部署」任务：

- 部署时下载制品：**不勾选**（镜像已在 ACR，不需要制品）
- 制品：留空
- 主机组：选应用机所在的生产主机组
- 执行用户：`root`
- 部署策略：不暂停，分批 1
- 变量进行编码：不勾选
- 部署脚本：首次把 `deploy/host-deploy.sh` 复制到应用机，线上任务执行
  `bash /opt/dreamer/project/training-admin/host-deploy.sh`；也可以直接把脚本内容粘进去

脚本内容（在应用机执行）：

```bash
set -eu
cd /opt/dreamer/project/training-admin
docker compose pull admin
docker compose up -d admin
docker exec training-admin wget -qO- http://127.0.0.1/healthz
```

首次需要：应用机 `docker login` 一次 ACR，目录里已有 `docker-compose.yml` 和 `.env`。
脚本会等待容器健康检查通过，失败时打印最近 50 行日志并返回非 0。

### 4. 冒烟检查（可选）

```bash
curl -fsS https://training.dreamerlab.cn/admin/ >/dev/null
curl -fsS https://training.dreamerlab.cn/api/v1/health >/dev/null
```

Nginx 接入是**一次性手动操作**，不要在流水线里改 Nginx；按「首次部署」第 3 步做一次即可。

## 约定

- 权限只影响界面，安全由后端按契约的 `x-roles` 保证。
- 后台页面带「账号名 + 时间」水印；手机号默认脱敏。
- 颜色、字号只从 `src/ui-tokens` 取，不写死。
- 掌握度、预估分、额度、会员等业务结果只展示服务端返回值，不在前端计算。
- 新增依赖写理由，只用主流、活跃维护的库。

## 官网

官网（`https://training.dreamerlab.cn/`）的源码在 `site/`，和后台打进同一个镜像，由同一个 `training-admin` 容器托管。

```bash
pnpm site:dev     # 构建并预览 http://localhost:4173，/api 转发到线上公开接口
pnpm site:build   # 只构建到 dist-site/
```

页面：首页 `/`、下载 `/download`、会员 `/pricing`、帮助 `/help`、关于 `/about`、协议 `/legal/user`、`/legal/privacy`、`/legal/membership`。

上线前要做的事：

1. 在 `site/site.config.json` 填公安备案号（ICP 备案号已填；没填的项不显示）
2. 宿主机 dreamer-nginx 的 `training.dreamerlab.cn` 443 server 块里，加入 `deploy/nginx-admin.conf` 里「官网」那一段 location，然后 `nginx -t && nginx -s reload`。这一段只接管官网用到的路径，`/api` 不受影响
3. 下载按钮的地址写在 `site/site.config.json` 的 `androidUrl`、`iosUrl`；没填的取后台「App 最新版本」配置（`/bootstrap` 的 `update.download_url`），都没有时下载页显示「添加客服企业微信获取」
4. 客服二维码取自后台「购买入口」的安卓配置（`/purchase-entry?platform=android`）
