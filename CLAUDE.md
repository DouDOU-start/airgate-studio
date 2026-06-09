# airgate-studio — Claude 开发指南

> 叠加在 monorepo 根 `../CLAUDE.md` 之上。完整流程见共享 skill **`develop-plugin`**；接口契约见 `../airgate-sdk/CLAUDE.md`。

- **插件身份**：type `extension`，作用 = 多模态内容创作。
- 实现 `sdk.ExtensionPlugin`：自定义 API / 后台任务（含图像等生成任务）；要调模型能力经 `Host.Invoke`。
- 元信息在 `backend/internal/studio/metadata.go`（id/name 为 `PluginID`/`PluginName` 常量）。
- 涉及生成任务/图像规则时，参考 `../airgate-core/docs/architecture/` 下任务状态机与生成任务相关文档。

## 🚫 红线

- 只依赖 `airgate-sdk`，禁止 import core 内部；用 core 能力经 `Host.Invoke`/`InvokeStream`。
- `plugin.yaml` 由 `make manifest` 生成，不可手改。
- 前端单 `index.js` → `web/dist/index.js`，用 `@doudou-start/airgate-theme`。

## 命令

`make dev`（独立调试）· `make manifest` · `make build` · `make ci` · `make release`
