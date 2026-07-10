# AirGate Studio 独立部署 Makefile（standalone-gateway 分支：不再是插件）

GO := GOTOOLCHAIN=local go

WEBDIST := backend/internal/studio/webdist

.PHONY: help install dev dev-backend dev-web build build-web sync-webdist ensure-webdist ci pre-commit lint type-check test vet fmt setup-hooks clean

help: ## 显示帮助信息
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# ===================== 构建 =====================

install: ## 安装前后端依赖
	cd web && pnpm install
	cd backend && $(GO) mod download
	@echo "依赖安装完成"

build: sync-webdist ## 完整构建：前端 → 嵌入后端 → 编译单二进制
	mkdir -p bin
	cd backend && GOWORK=off $(GO) build -o ../bin/airgate-studio ./cmd/airgate-studio
	@echo "构建完成: bin/airgate-studio"

build-web: ## 构建前端 SPA
	cd web && pnpm build

# sync-webdist：生产构建路径上的"权威"同步点。
# 依赖 build-web → 每次都会先 pnpm build，保证 web/dist 是当前源码产物，
# 然后 rm -rf + cp -r 把 webdist 强制刷新。生产二进制必经此处。
sync-webdist: build-web
	rm -rf $(WEBDIST)
	cp -r web/dist $(WEBDIST)
	touch $(WEBDIST)/.gitkeep
	@echo "已强制同步 web/dist → $(WEBDIST)"

# ensure-webdist：轻量 bootstrap，仅为 test / vet 保底 //go:embed 编译，
# 不触发 pnpm。生产构建走 sync-webdist。
ensure-webdist:
	@if [ ! "$$(ls -A $(WEBDIST) 2>/dev/null)" ]; then \
		mkdir -p $(WEBDIST); \
		touch $(WEBDIST)/.gitkeep; \
		echo "webdist 为空，写入 placeholder（仅供后端 test/vet 编译用）"; \
	fi

# ===================== 开发 =====================

dev: ## 本地开发：后端 go run（:8181，读 backend/config.yaml）+ 前端 vite dev（:5174 --host）
	@if [ ! -f backend/config.yaml ]; then \
		echo "缺少 backend/config.yaml，请先：cp backend/config.yaml.example backend/config.yaml 并按需修改"; \
		exit 1; \
	fi
	@trap 'kill 0' EXIT; \
	(cd backend && GOWORK=off $(GO) run ./cmd/airgate-studio) & \
	(cd web && pnpm dev --host) & \
	wait

dev-backend: ## 仅后端（读 backend/config.yaml；env 可覆盖单项）
	cd backend && GOWORK=off $(GO) run ./cmd/airgate-studio

dev-web: ## 仅前端 vite dev server（--host 允许非本机 IP 访问）
	cd web && pnpm dev --host

# ===================== 质量检查 =====================

ci: ensure-webdist lint vet test build ## 本地运行与 CI 完全一致的检查

pre-commit: ensure-webdist lint test vet ## pre-commit hook 调用

lint: ## 代码检查（需要安装 golangci-lint）
	@if ! command -v golangci-lint > /dev/null 2>&1; then \
		echo "错误: 未安装 golangci-lint，请执行: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest"; \
		exit 1; \
	fi
	@cd backend && GOWORK=off golangci-lint run ./...
	@cd web && pnpm exec tsc --noEmit
	@cd web && pnpm lint
	@echo "代码检查通过"

type-check: ## 前端 TypeScript 类型检查
	cd web && pnpm type-check

test: ensure-webdist ## 运行后端测试
	cd backend && GOWORK=off $(GO) test ./...

vet: ensure-webdist ## 静态分析
	cd backend && GOWORK=off $(GO) vet ./...

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
