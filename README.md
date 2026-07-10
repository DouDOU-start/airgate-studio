<div align="center">
  <h1>AirGate Studio</h1>

  <p><strong>独立部署的多模态内容创作应用</strong></p>

  <p>
    <a href="https://github.com/DouDOU-start/airgate-studio/releases"><img src="https://img.shields.io/github/v/release/DouDOU-start/airgate-studio?style=flat-square" alt="release" /></a>
    <a href="https://github.com/DouDOU-start/airgate-studio/blob/master/LICENSE"><img src="https://img.shields.io/github/license/DouDOU-start/airgate-studio?style=flat-square" alt="license" /></a>
    <a href="https://github.com/DouDOU-start/airgate-studio/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/DouDOU-start/airgate-studio/ci.yml?branch=master&style=flat-square&label=CI" alt="ci" /></a>
    <img src="https://img.shields.io/badge/Go-1.25-00ADD8?style=flat-square&logo=go" alt="go" />
    <img src="https://img.shields.io/badge/React-19-61DAFB?style=flat-square&logo=react" alt="react" />
  </p>
</div>

---

> **standalone-gateway 分支**：本仓已从「AirGate 插件」改造为**独立部署的单体 Web 应用**。
> 独立 Go HTTP 服务 + 自带 SPA 前端；用户身份经 AirGate（core）的 OAuth2 授权码 + PKCE
> 单点登录；生成任务存自己的 Postgres、由自带 worker 拿用户的 sk- key 按**多协议策略**
> 调 core 生图端点（Imagen `:predict` / gemini `:generateContent` / gpt-image、dall-e
> `/v1/images` / 其余回退 chat 多模态）；产物图片存本地磁盘。不依赖 `airgate-sdk`。

## ✨ 核心特性

- **图像创作**：文生图、图生图、局部重绘（掩码编辑器）、批量生成
- **任务管理**：自带 Postgres 任务表 + 轮询 worker（领取 / 失败重试 / 重启恢复）
- **单点登录**：OAuth2 授权码 + PKCE 对接 AirGate，自动按分组领取用户专属 sk- key
- **分组×模型货架**：管理员开放分组、按组同步/上架模型；用户按「分组+模型」创作，计费按组倍率走 core
- **本地资产**：生成图片落盘 `{DATA_DIR}/assets/generated/`，经 `/assets-runtime/` 提供

## 🧭 架构

```text
浏览器 ──▶ airgate-studio（本仓，单二进制，内嵌 SPA）
             │  /auth/*：OAuth2 + PKCE 单点登录（core /oauth/authorize|token|userinfo|provision-key）
             │  /api/*：会话 cookie 鉴权的业务 API（含管理端货架接口）
             │  worker：轮询 studio_tasks，拿用户按组 sk- key 按模型协议策略调 core 生图端点
             ▼
        AirGate core（渠道调度 + 计费） ──▶ 上游多模态图像模型
```

## 🚦 路由

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/auth/login` · `/auth/callback` | OAuth2 登录 / 回调 |
| POST | `/auth/logout` | 退出登录 |
| GET | `/api/user/info` | 会话用户 + 余额 |
| POST / GET | `/api/generation-tasks` | 创建 / 列出生成任务 |
| GET / DELETE | `/api/generation-tasks/{id}` | 读取 / 删除生成任务 |
| GET | `/api/groups` | 已开放分组 ×（本人 key 就绪状态） |
| GET | `/api/models` | 带 `group_id` 读分组货架；不带透传 core `/v1/models` |
| GET / PUT / POST | `/api/admin/groups*` · `/api/admin/models*` | 管理端：分组开关、按组同步/上架模型（config 白名单） |
| GET | `/assets-runtime/generated/{file}` | 生成产物图片 |
| * | `/*` | SPA 前端（fallback index.html） |

## ⚙️ 配置

装载顺序：默认值 → `config.yaml`（`CONFIG_PATH` 指定路径，默认读工作目录下 `config.yaml`，缺省则跳过）→ 环境变量覆盖单项。推荐用 `config.yaml`，放 `backend/` 下（样例见 [`backend/config.yaml.example`](backend/config.yaml.example)，含密钥、勿提交）；纯环境变量部署见 [`.env.example`](.env.example)。

| yaml 键 | env | 必填 | 说明 |
|---|---|---|---|
| `listen_addr` | `LISTEN_ADDR` | 否 | 监听地址，默认 `:8181` |
| `database_url` | `DATABASE_URL` | 是 | Postgres 连接串 |
| `airgate_base_url` | `AIRGATE_BASE_URL` | 是 | core 地址（服务端调用） |
| `airgate_public_url` | `AIRGATE_PUBLIC_URL` | 否 | 浏览器可达的 core 地址，默认同 base_url |
| `oauth_client_id` / `oauth_client_secret` | `OAUTH_CLIENT_ID` / `OAUTH_CLIENT_SECRET` | 是 | core 侧登记的 OAuth 客户端凭据 |
| `public_base_url` | `PUBLIC_BASE_URL` | 是 | 本应用对外地址（拼 redirect_uri） |
| `session_secret` | `SESSION_SECRET` | 是 | 会话 cookie HMAC 密钥 |
| `data_dir` | `DATA_DIR` | 否 | 数据目录，默认 `data` |
| `admin_airgate_user_ids` | `ADMIN_AIRGATE_USER_IDS` | 否 | studio 管理员的 core 用户 ID 白名单（管理控制台） |

## 📁 目录结构

```text
backend/
  cmd/airgate-studio/   服务入口
  internal/studio/      全部实现：OAuth 会话、任务仓储、货架、worker、策略分发、资产、路由、SPA 托管
web/
  src/lib/              API 客户端与通用工具
  src/components/       顶层组件（用户区、管理控制台）
  src/studio/           创作中心（视图、上下文、画廊、掩码编辑器、模型能力）
```

## 🚀 构建与开发

```bash
make install   # 安装前后端依赖
make build     # 前端构建 → 嵌入 → 单二进制 bin/airgate-studio
make ci        # lint + vet + test + build（与 CI 一致）
make dev       # 后端 go run(:8181，读 backend/config.yaml) + 前端 vite dev(:5174 --host)
```

部署即运行单二进制：准备好 Postgres 与上述环境变量后 `./bin/airgate-studio`。

## 🤝 相关文档

- 开发护栏：[`CLAUDE.md`](CLAUDE.md)
- core 对接说明与演进规划：[`docs/core-integration.md`](docs/core-integration.md)

## 📜 License

MIT — 详见 [LICENSE](LICENSE)。
