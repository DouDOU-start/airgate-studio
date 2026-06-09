# AirGate 创作中心插件 Makefile

GO := GOTOOLCHAIN=local go

WEBDIST := backend/internal/studio/webdist

.PHONY: help install build build-web build-backend release dev ensure-webdist sync-webdist clean test vet ci pre-commit lint type-check fmt setup-hooks

help: ## 显示帮助信息
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# ===================== 构建 =====================

install: ## 安装前后端依赖
	cd web && pnpm install
	cd backend && $(GO) mod download
	@echo "依赖安装完成"

build: build-backend ## 完整构建：前端 → 嵌入后端 → 编译

build-web: ## 构建插件前端
	cd web && pnpm build

# build-backend 走 sync-webdist：必先 pnpm build、再 force-sync。
build-backend: sync-webdist ## 构建后端二进制
	mkdir -p bin
	cd backend && $(GO) build -o ../bin/airgate-studio .

# release 与 build-backend 共用 sync-webdist，不再内联 rm/cp 同步逻辑。
release: sync-webdist ## 编译 Linux amd64 版本（用于上传到 marketplace）
	mkdir -p bin
	cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -buildvcs=false -trimpath -ldflags "-X 'github.com/DouDOU-start/airgate-studio/backend/internal/studio.PluginVersion=$${VERSION:-dev}'" -o ../bin/airgate-studio-linux-amd64 .
	@echo "构建完成: bin/airgate-studio-linux-amd64"

# sync-webdist：生产构建路径上的"权威"同步点。
# 依赖 build-web → 每次都会先 pnpm build，保证 web/dist 是当前源码产物，
# 然后 rm -rf + cp -r 把 webdist 强制刷新。生产二进制必经此处。
#
# 与 ensure-webdist 拆成两个独立 target 是有意为之——它们都是 phony，但
# make 只对"同名 phony"去重；一旦 ci 链路里 vet/test 先触发了 ensure-webdist
# 的 placeholder，再去 build-backend 时如果共用同一 target，会被去重跳过、
# 把 placeholder 嵌进二进制。拆开后 sync-webdist 与 ensure-webdist 各自
# 独立调度，互不干扰。
sync-webdist: build-web
	rm -rf $(WEBDIST)
	cp -r web/dist $(WEBDIST)
	touch $(WEBDIST)/.gitkeep
	@echo "已强制同步 web/dist → $(WEBDIST)"

# ensure-webdist：轻量 bootstrap，仅为 test / vet 这类不需要真实前端内容
# 的目标保底 //go:embed 编译。不触发 pnpm，避免后端单测受前端依赖牵连。
# *不保证* webdist 是 fresh —— 生产构建走 sync-webdist。
ensure-webdist:
	@if [ ! "$$(ls -A $(WEBDIST) 2>/dev/null)" ]; then \
		mkdir -p $(WEBDIST); \
		touch $(WEBDIST)/.gitkeep; \
		echo "webdist 为空，写入 placeholder（仅供后端 test/vet 编译用）"; \
	fi

# ===================== 开发 =====================

dev: build-web ## 构建前端资产并提示如何在 core 里 dev 加载本插件
	@echo "在 airgate-core/backend/config.yaml 的 plugins.dev 节追加："
	@echo ""
	@echo "  plugins:"
	@echo "    dev:"
	@echo "      - name: airgate-studio"
	@echo "        path: $(realpath ./backend)"
	@echo ""
	@echo "然后启动 core: cd airgate-core/backend && go run ./cmd/server"

# ===================== 质量检查 =====================

ci: ensure-webdist lint vet test build-backend ## 本地运行与 CI 完全一致的检查

pre-commit: ensure-webdist lint test vet ## pre-commit hook 调用

lint: ## 代码检查（需要安装 golangci-lint）
	@if ! command -v golangci-lint > /dev/null 2>&1; then \
		echo "错误: 未安装 golangci-lint，请执行: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest"; \
		exit 1; \
	fi
	@cd backend && golangci-lint run ./...
	@cd web && pnpm exec tsc --noEmit
	@cd web && pnpm lint
	@echo "代码检查通过"

type-check: ## 前端 TypeScript 类型检查
	cd web && pnpm type-check

test: ensure-webdist ## 运行后端测试
	cd backend && $(GO) test ./...

vet: ensure-webdist ## 静态分析
	cd backend && $(GO) vet ./...

fmt: ## 格式化后端代码
	cd backend && $(GO) fmt ./...

# ===================== Git Hooks =====================

setup-hooks: ## 安装 Git hooks（pre-commit + commit-msg）
	@echo '#!/bin/sh' > .git/hooks/pre-commit
	@echo 'make pre-commit' >> .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@cp scripts/commit-msg .git/hooks/commit-msg
	@chmod +x .git/hooks/commit-msg
	@echo "Git hooks 已安装（pre-commit + commit-msg）"

# ===================== 清理 =====================

clean: ## 清理构建产物
	rm -rf bin/ web/dist
	rm -rf $(WEBDIST)
	mkdir -p $(WEBDIST)
	touch $(WEBDIST)/.gitkeep
