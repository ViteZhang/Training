# Go 二进制由云效 Go 构建任务生成到 bin/；这里负责组装运行镜像。
# FONT_IMAGE 和 RUNTIME_IMAGE 在国内构建时可传入 ACR 中同步的镜像地址。
ARG FONT_IMAGE=golang:1.27
ARG RUNTIME_IMAGE=gcr.io/distroless/static-debian12:nonroot

FROM ${FONT_IMAGE} AS fonts
# 导出题库的 PDF 需要 TrueType 字体：中文 Droid Sans Fallback（Apache 2.0）、字母数字 DejaVu Sans（自由许可）；
# 运行镜像不含字体，从构建阶段复制过去。
RUN apt-get update && apt-get install -y --no-install-recommends fonts-droid-fallback fonts-dejavu-core && rm -rf /var/lib/apt/lists/*

# distroless 自带 CA 证书与时区数据，不含 shell，以非 root 运行。
# 用 CMD 而不是 ENTRYPOINT：compose 里 command 写完整命令（/app/api、/app/worker、/app/api migrate up）。
FROM ${RUNTIME_IMAGE}
WORKDIR /app
COPY bin/api bin/worker /app/
COPY --from=fonts /usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf /usr/share/fonts/truetype/dejavu/DejaVuSans.ttf /app/fonts/
EXPOSE 8080
USER nonroot:nonroot
CMD ["/app/api"]
