# airgate-studio — Claude 开发指南

> 叠加在 monorepo 根 `../CLAUDE.md` 之上。完整流程见共享 skill **`develop-plugin`**；接口契约见 `../airgate-sdk/CLAUDE.md`。

- **插件身份**：id `airgate-studio`，type `extension`，作用 = 多模态内容创作。
- 实现 `sdk.ExtensionPlugin`：自定义 API / 后台任务（含图像等生成任务）；要调模型能力经 `Host.Invoke`。
- 元信息在 `backend/internal/studio/metadata.go`（id/name 为 `PluginID`/`PluginName` 常量）。
- 涉及生成任务/图像规则时，参考 skill `core-dev`「任务状态机」与生成任务相关规则。

## 🚫 红线

通用边界铁律（只依赖 `airgate-sdk`、经 `Host.Invoke`/`InvokeStream` 调 core、`plugin.yaml` 由 `make manifest` 生成不可手改、前端单 `index.js` bundle）见 skill **`develop-plugin`「🚫 边界铁律」**。

## 命令

构建/发布命令见 skill **`develop-plugin`「构建 / 发布」**；本仓实际 make 目标以 `Makefile` 为准。
