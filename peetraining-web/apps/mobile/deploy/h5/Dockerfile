# H5 版镜像：只有 Nginx 和静态网页。构建上下文是流水线里准备好的 h5-package/（dist-h5/ 与 nginx.conf）。
# 网页在流水线里先构建好（scripts/build-h5.sh），这里只负责打包，所以镜像里没有 Node 和源码。
# 与 peetraining-admin 一致：基础镜像从 ACR 拉，云效构建机直连 Docker Hub 会超时。
ARG NGINX_IMAGE=crpi-3mjxc296umgnl4w9.cn-beijing.personal.cr.aliyuncs.com/dreamer-lab/nginx:1.27-alpine
FROM ${NGINX_IMAGE}
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY dist-h5/ /usr/share/nginx/html/h5/
