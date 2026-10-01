# 常用命令见 CLAUDE.md「常用命令」。所有目标都不需要真实密钥。
SHELL := /bin/bash
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X peetraining-server/internal/app.Version=$(VERSION)
TOOL := go tool -modfile=tools/go.mod
DEV_COMPOSE := docker compose -f docker-compose.dev.yml
DEV_ENV := APP_ENV=local MYSQL_DSN='training:training@tcp(127.0.0.1:3306)/training' REDIS_ADDR=127.0.0.1:6379

.PHONY: dev dev-deps dev-down gen gen-check test test-unit lint migrate-new migrate-up migrate-status eval build docker api-tag

## dev：起本地 MySQL 与 Redis，执行迁移，再同时启动 API（:8080）与 Worker；Ctrl+C 一起停止
dev: dev-deps
	$(DEV_ENV) go run ./cmd/api migrate up
	@trap 'kill 0' INT TERM EXIT; \
	$(DEV_ENV) go run ./cmd/worker & \
	$(DEV_ENV) go run ./cmd/api & \
	wait

dev-deps:
	$(DEV_COMPOSE) up -d --wait

dev-down:
	$(DEV_COMPOSE) down

## gen：按 api/openapi.yaml 生成 internal/gen，按 db/queries 生成 internal/dbq
gen:
	$(TOOL) oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml
	$(TOOL) sqlc generate
	go run ./internal/http/stubgen

## gen-check：生成后不应有未提交的差异（流水线用）
gen-check: gen
	git diff --exit-code -- internal/gen internal/dbq internal/http/stubs_gen.go

## test：全部测试，含 testcontainers 集成测试（需要 Docker；流水线设 REQUIRE_INTEGRATION=1）
test:
	go test -race -count=1 ./...

test-unit:
	go test -short -count=1 ./...

## lint：gofmt、go vet、golangci-lint
lint:
	@test -z "$$(gofmt -l . | tee /dev/stderr)" || (echo "以上文件需要 gofmt" && exit 1)
	go vet ./...
	$(TOOL) golangci-lint run ./...

## migrate-new name=xxx：新建顺序编号的迁移文件
migrate-new:
	@test -n "$(name)" || (echo "用法：make migrate-new name=add_users" && exit 1)
	go run ./cmd/api migrate new $(name)

migrate-up:
	$(DEV_ENV) go run ./cmd/api migrate up

migrate-status:
	$(DEV_ENV) go run ./cmd/api migrate status

## eval cap=import|kp|grading|essay|ocr：跑 AI 评测（各项在对应卡里实现）
eval:
	go run ./cmd/eval -cap=$(cap)

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/api ./cmd/api
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/worker ./cmd/worker

docker:
	docker build --build-arg VERSION=$(VERSION) -t peetraining-server:$(VERSION) .

## api-tag v=api-v0.3：给接口契约打标签，供前端 pnpm gen:api 拉取
api-tag:
	@test -n "$(v)" || (echo "用法：make api-tag v=api-v0.3" && exit 1)
	@[[ "$(v)" =~ ^api-v[0-9]+\.[0-9]+(\.[0-9]+)?$$ ]] || (echo "标签格式应为 api-vX.Y" && exit 1)
	@git diff --quiet -- api/openapi.yaml || (echo "api/openapi.yaml 有未提交的修改" && exit 1)
	git tag -a $(v) -m "接口契约 $(v)"
	git push origin $(v)
