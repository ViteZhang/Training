#!/usr/bin/env bash
# 可选质量检查脚本：检查生成代码、lint 和全部测试；发布流水线不调用它。
# 它需要 tools/go.mod 对应的开发工具和 Docker（集成测试用 testcontainers 起 MySQL / Redis）。
# 构建机没有 Docker 时去掉 REQUIRE_INTEGRATION=1 改跑 make test-unit。
set -euo pipefail
export GOPROXY=${GOPROXY:-https://goproxy.cn,direct}
make gen-check
make lint
REQUIRE_INTEGRATION=${REQUIRE_INTEGRATION:-1} make test
