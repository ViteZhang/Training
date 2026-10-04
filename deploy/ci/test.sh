#!/usr/bin/env bash
# 云效流水线「测试」阶段：检查生成代码没漏提交、lint、全部测试。
# 构建机需要 Go 1.27（go.mod 会自动下载工具链）与 Docker（集成测试用 testcontainers 起 MySQL / Redis）。
# 构建机没有 Docker 时去掉 REQUIRE_INTEGRATION=1 改跑 make test-unit，并在发布前本地跑一次 make test。
set -euo pipefail
export GOPROXY=${GOPROXY:-https://goproxy.cn,direct}
make gen-check
make lint
REQUIRE_INTEGRATION=${REQUIRE_INTEGRATION:-1} make test
