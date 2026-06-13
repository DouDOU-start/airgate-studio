# AirGate Studio（创作中心）

AirGate 扩展插件：面向图片、视频、音频等多模态内容生成的统一创作中心。生成任务统一走 Core 任务状态机，由网关插件执行上游调用，本插件专注创作流程与结果管理。

- 插件 ID：`airgate-studio` · 类型：`extension`
- 依赖：AirGate Core（任务/模型目录/用户能力经 `Host.Invoke`）；生成执行依赖网关插件（当前为 `gateway-openai`）

## 功能

- **图像创作**：文生图、图像编辑、局部重绘（掩码编辑器）——已实现
- **任务管理**：生成任务创建、状态跟踪、列表与结果库（基于 Core 统一任务状态机，可恢复、可审计）
- **模型选择**：按平台模型目录动态列出可用生成模型
- 视频、音乐生成：类型已预留，规划中

前端入口：平台内 `/studio` 页面（单页应用，含创作区、掩码编辑、结果库）。

## 构建

```bash
make install   # 安装前后端依赖
make dev       # devserver 独立调试（脱离 Core）
make build     # 前端 bundle → 嵌入 → Go 二进制
make ci        # lint + type-check + test + vet + build
make release   # 交叉编译 linux-amd64
```

产出的二进制由 AirGate Core 作为 gRPC 子进程加载；前端为单 `index.js` bundle，由 Core 统一提供资产服务。

## 权限声明

插件按最小权限声明 Host 能力（见 `backend/internal/studio/metadata.go`）：`tasks.create` / `tasks.get` / `tasks.list` / `platforms.list` / `models.list` / `users.get`。

## 目录结构

```
backend/   Go 插件实现（internal/studio/：路由、生成任务编排、Host 调用）
web/       前端（React 19 + Vite），输出 web/dist/index.js
```

## 相关文档

- 开发护栏：[`CLAUDE.md`](CLAUDE.md)
- 任务子系统设计：`../airgate-core/docs/architecture/task-state-machine.md`
- 插件契约与生态架构：`../airgate-core/docs/architecture/current/`
