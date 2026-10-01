# 多阶段构建：api 与 worker 在同一个镜像里，用启动命令区分（/app/api、/app/worker）。
#
# 云效流水线在国内构建：基础镜像建议换成 ACR 里的同步镜像（--build-arg GO_IMAGE=... RUNTIME_IMAGE=...），
# Go 模块走国内代理（GOPROXY 默认 goproxy.cn）。具体在 T04 的流水线配置与 runbook 里写明。
ARG GO_IMAGE=golang:1.27
ARG RUNTIME_IMAGE=gcr.io/distroless/static-debian12:nonroot

FROM ${GO_IMAGE} AS build
WORKDIR /src
ARG GOPROXY=https://goproxy.cn,direct
ENV CGO_ENABLED=0 GOTOOLCHAIN=local GOPROXY=${GOPROXY}
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN go build -trimpath -ldflags "-s -w -X peetraining-server/internal/app.Version=${VERSION}" -o /out/api ./cmd/api \
 && go build -trimpath -ldflags "-s -w -X peetraining-server/internal/app.Version=${VERSION}" -o /out/worker ./cmd/worker

# distroless 自带 CA 证书与时区数据，不含 shell，以非 root 运行。
FROM ${RUNTIME_IMAGE}
WORKDIR /app
COPY --from=build /out/api /out/worker /app/
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/api"]
