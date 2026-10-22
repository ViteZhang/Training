# Go 二进制由云效 Go 构建任务生成到 bin/；这里负责组装运行镜像。
# 两个镜像地址必须显式传入 ACR 中的镜像，避免默认回源 Docker Hub 或 GCR。
ARG FONT_IMAGE
ARG RUNTIME_IMAGE

FROM ${FONT_IMAGE} AS fonts
# FONT_IMAGE 是一次性构建并推送到 ACR 的字体资源镜像，日常发布不再运行 apt-get。

# distroless 自带 CA 证书与时区数据，不含 shell，以非 root 运行。
# 用 CMD 而不是 ENTRYPOINT：compose 里 command 写完整命令（/app/api、/app/worker、/app/api migrate up）。
FROM ${RUNTIME_IMAGE}
WORKDIR /app
COPY bin/api bin/worker /app/
COPY --from=fonts /usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf /usr/share/fonts/truetype/dejavu/DejaVuSans.ttf /app/fonts/
EXPOSE 8080
USER nonroot:nonroot
CMD ["/app/api"]
