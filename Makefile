# 常用命令见 CLAUDE.md「常用命令」。所有目标都不需要真实密钥。
SHELL := /bin/bash
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X peetraining-server/internal/app.Version=$(VERSION)
TOOL := go tool -modfile=tools/go.mod
.PHONY: dev dev-worker dev-migrate gen gen-check test test-unit lint migrate-new migrate-up migrate-status eval eval-all build docker api-tag

## dev：直连 .env.local 配置的 ECS MySQL 与 Redis，启动本地 API；Ctrl+C 停止
dev:
	./scripts/dev.sh

## dev-worker：同时启动 Worker（会消费共享 Redis 队列）
dev-worker:
	./scripts/dev.sh --worker

## dev-migrate：对 .env.local 配置的数据库执行迁移（显式操作）
dev-migrate:
	./scripts/dev.sh --migrate up

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
	./scripts/dev.sh --migrate up

migrate-status:
	./scripts/dev.sh --migrate status

## eval cap=import|kp|rubric|grading|essay|ocr [args="-model qwen-max -prompt v2 -record -note 改了什么"]：跑 AI 评测（ADR 0012）
eval:
	go run ./cmd/eval -cap=$(cap) $(args)

## eval-all：依次跑全部评测（有一项不达标时退出码非 0，但会跑完）
eval-all:
	@fail=0; for c in import kp rubric grading essay ocr; do echo "== $$c"; go run ./cmd/eval -cap=$$c $(args) || fail=1; done; exit $$fail

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
