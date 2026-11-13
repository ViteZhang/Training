# H5 版镜像：只有 Nginx 和静态网页。构建上下文是流水线里准备好的 h5-package/（dist-h5/ 与 nginx.conf）。
# 网页在流水线里先构建好（scripts/build-h5.sh），这里只负责打包，所以镜像里没有 Node 和源码。
ARG NGINX_IMAGE=docker.1ms.run/library/nginx:1.27-alpine
FROM ${NGINX_IMAGE}
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY dist-h5/ /usr/share/nginx/html/h5/
