# studio ↔ core 对接说明（分组×模型货架）

> 状态：本文描述**已实现**的对接形态（2026-07 落地）。实现细节以代码与 `CLAUDE.md` 为准；
> 本文侧重跨仓视角——studio 与 core 的契约、边界与设计取舍。

## 一、对接全景

studio 是独立部署的单体应用，自有 Postgres（`studio_*` 表）与本地资产目录，
与 core 的全部联系只有 HTTP，分两个面：

### 1. 身份面（OAuth2 授权码 + PKCE + 按组领 key）

core 是唯一身份源（IdP）。studio 在 core 管理后台登记为 OAuth 应用（client_id/secret + redirect_uri 白名单）。

```text
浏览器                     studio 后端                        core
   │ GET /auth/login          │                                │
   │◀─ 302 ──────────────────│ 生成 state+PKCE verifier        │
   │── GET {core}/oauth/authorize?... ─────────────────────────▶│ 登录 + 同意页（first_party 静默）
   │◀─ 302 {studio}/auth/callback?code&state ──────────────────│
   │── GET /auth/callback ───▶│                                │
   │                          │── POST /oauth/token ───────────▶│
   │                          │── GET  /oauth/userinfo ────────▶│ → sub/name/email + groups[]（可用分组）
   │                          │── POST /oauth/provision-key ───▶│ → 默认分组 sk- key（回填 group_id）
   │                          │── POST /oauth/provision-key {group_id} ×N ─▶│ 按组补领
   │                          │ upsert studio_users + studio_user_keys      │
   │◀─ 302 / + 会话 cookie ───│（7 天 HMAC 签名，无服务端 session）           │
```

要点：

- **provision-key 幂等键 = 用户×应用×分组**（core 侧 2026-07 扩展），同一应用可按分组为
  用户领多把 key；响应回填实际落点 `group_id`。
- **按组补领的范围**：管理员登录领「用户全部可用分组」（同步模型要用），普通用户只领
  「管理员已开放 ∩ 用户可用」的分组；单组领取失败不阻断登录。
- **key 是纯管道**：明文只存 studio 后端（`studio_users.api_key` 默认组 + `studio_user_keys`
  按组），用户与管理员在界面上都不感知 key。
- key 被用户在 core 侧禁用（403）不阻断登录，任务执行时才报可读错误（与 playground 的
  「登录即拦截」是刻意差异）。
- 访问令牌 `oat_`（TTL 2h）仅在回调过程中使用，studio 不存储、无 refresh token。

### 2. 数据面（用户按组 sk- key 直调 core /v1）

生成任务由 studio worker 轮询执行，按任务的 `group_id` 取该用户对应分组的 key 调 core，
按「模型 → protocols」策略分发（gpt-image/dall-e → images 端点；Imagen → :predict；
其他 gemini → :generateContent；其余回退 chat 多模态）。

**职责边界（铁律）**：渠道选择、上游转发、计费扣费全在 core——key 属于 core 的某个
用户分组，core 按分组挑渠道、按倍率计费；studio 不感知渠道、不碰价格。

## 二、分组×模型货架（已实现）

### 设计取舍

「像 core 渠道管理一样可控」的落点不是在 studio 复制渠道管理（studio 只有一个上游 = core），
而是两层分工：

- **studio 管「货架」（软控制）**：管理员决定开放哪些分组、每组上架哪些模型、展示名/排序。
  纯展示层——用户绕过 UI 直接调 core 也只是花自己的钱，无安全问题。
- **core 管「仓库和收银」（硬控制）**：分组决定渠道池与倍率；用户选组 = studio 换用该组的
  key，计费自动按组价格走，core 转发链路零改动（保住「key 属组」不变量）。
- **明确不做**：studio 不接上游、不做渠道管理、不复制价目表、不自行计费。
- **用户选「渠道」的正确姿势**：core 渠道模型名重写（同一上游模型在不同渠道映射不同对外
  名字），用户仍然只选「分组+模型」；渠道拓扑对应用不可见（core 有意收口）。

### 落地形态

数据（studio 侧三张表）：

- `studio_groups`——core 分组**镜像**（id/名称/倍率/备注，管理员登录时自动收集自
  userinfo.groups）+ studio 自己的 `enabled` 开关。真正的分组定义永远以 core 为准。
- `studio_models`——按组货架：`(core_group_id, model_name)` 唯一，protocols 同步自
  core `/v1/models`，新同步默认下架，core 已下线的标 `missing_at_core`（去留由管理员定）。
- `studio_user_keys`——用户按组 key（登录回调自动 provision）。

接口：

| 角色 | 接口 | 说明 |
|---|---|---|
| 管理员（config `admin_airgate_user_ids` 白名单） | `GET/PUT /api/admin/groups*` | 分组镜像列表 / 开关 |
| | `POST /api/admin/groups/{id}/sync-models` | 用管理员本人该组的 key 拉 `/v1/models` 全量同步 |
| | `GET/PUT /api/admin/models*` | 货架列表 / 上架、展示名、排序 |
| 用户 | `GET /api/groups` | 已开放分组 ×（本人 key 就绪状态） |
| | `GET /api/models?group_id=`（必填） | 该组已上架模型（展示名+protocols） |
| | `POST /api/generation-tasks`（`group_id` 必填） | 三重校验：组开放 / 模型上架 / 持有该组 key |

**货架是唯一模型供给**：`/api/models` 不再透传 core 全量目录，用户也不能手动填写
模型名；未开放任何分组时前端展示空态引导联系管理员，任务创建被 `group_id` 必填拦截
（货架化之前创建的历史遗留任务仍按默认 key 兜底执行，见 `worker.go` 的 `resolveTaskKey`）。

### 边角语义（防回归）

- 管理员**关组/撤模型**只拦新任务：在途任务照跑，已领 key 原地保留（重新开放无需重登录）。
- 分组在用户登录后才开放 → 该用户 `key_ready=false`，前端置灰提示重新登录补领；任务侧
  同样报「请重新登录」。
- core 侧硬关停（禁分组/移出用户/渠道下光）由 core 强制，studio 任务失败并展示 core 错误。
- 同步是手动动作（管理员点按钮），core 新增模型不会自动上架——节奏可控是特性不是缺陷。

### core 侧配套（本次为此在 core 增加的两个接入面能力）

1. `/oauth/userinfo` 附带 `groups[]`（id/name/rate_multiplier/note）。
2. provision-key 幂等键扩为（用户×应用×分组）+ 响应回填 `group_id`；
   存量库旧的两维唯一索引由 `bootstrap.Migrate` 的 legacyFixups 定点清理。
