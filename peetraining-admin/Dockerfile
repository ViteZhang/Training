# 管理后台 + 官网：Node 构建 + Nginx 静态托管，同一个镜像、同一个容器。
# 后台挂在 https://training.dreamerlab.cn/admin/，官网挂在根路径；容器只监听 80，由宿主机 dreamer-nginx 反代。
# 构建：docker build -t <ACR>/peetraining-admin:<tag> .
# 基础镜像固定从 ACR 拉取：云效构建机直连 Docker Hub 超时，公网镜像站速度不稳定。
FROM crpi-3mjxc296umgnl4w9.cn-beijing.personal.cr.aliyuncs.com/dreamer-lab/node:22-alpine AS build
WORKDIR /app

# corepack 下载 pnpm 本身也走国内源（云效构建机直连 registry.npmjs.org 超时）。
ENV COREPACK_NPM_REGISTRY=https://registry.npmmirror.com
RUN corepack enable && corepack prepare pnpm@10.28.0 --activate

# 先装依赖，源码变化时复用缓存。
COPY package.json pnpm-lock.yaml .npmrc ./
RUN pnpm install --frozen-lockfile

COPY tsconfig.json vite.config.ts index.html ./
COPY src ./src

RUN pnpm build

# 官网是纯静态页面，构建脚本不依赖任何第三方包。
COPY scripts/build-site.mjs ./scripts/build-site.mjs
COPY site ./site
RUN node scripts/build-site.mjs

FROM crpi-3mjxc296umgnl4w9.cn-beijing.personal.cr.aliyuncs.com/dreamer-lab/nginx:1.27-alpine

COPY deploy/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist-site /usr/share/nginx/html
COPY --from=build /app/dist /usr/share/nginx/html/admin

EXPOSE 80

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1/healthz || exit 1
