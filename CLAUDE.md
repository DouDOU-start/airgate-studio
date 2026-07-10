# airgate-studio — Claude 开发指南（standalone-gateway 分支）

> **本分支是独立部署的单体 Web 应用**（多模态内容创作产品，核心是图像生成任务），
> 与 master 插件形态长期分叉、不合回。
> **根 `../CLAUDE.md` 的「插件边界」「Host.Invoke」「生态职责速查表」及 skill
> `develop-plugin` / `core-dev` 描述的是 master 插件线，对本分支一律不适用。**
> 禁止 import 任何 `airgate-sdk` / `airgate-core` 包（前端 `@doudou-start/airgate-theme`
> npm 包是纯样式 token 库，例外保留）。

## 架构

独立 Go HTTP 服务（stdlib `net/http`，无 gin）+ 自带 SPA 前端（React 19 + Vite）。
用户身份经 core 的 OAuth2 授权码 + PKCE 单点登录；生成任务存自己的 Postgres 并由
自带 worker 执行；执行时拿该用户的 sk- key 按**多协议策略**调 core 生图端点
（core 零翻译纯透传）：Imagen 系 `:predict`、其他 gemini 图像模型 `:generateContent`、
gpt-image/dall-e 系 `/v1/images/generations|edits`、其余回退 `/v1/chat/completions`
多模态；产物图片存本地磁盘。

```
浏览器 ── /auth/login ──▶ core /oauth/authorize（授权码+PKCE）
   ▲                          │
   └── 会话 cookie（7 天 HMAC）◀── /auth/callback：换 token → userinfo →
                                   provision-key（领 sk- key）→ upsert studio_users
浏览器 ── /api/generation-tasks ──▶ studio_tasks（status=pending）
                                        │
                       worker 轮询（FOR UPDATE SKIP LOCKED 领取）
                                        │
        用该用户 api_key 按策略调 core 生图端点（strategy.go 判定：
        model→protocols 目录 TTL 缓存 + 模型名匹配，取不到按前缀启发式兜底）
                                        │
          各路径响应归一为 images + usage（genimage_*.go / worker.go）
                                        │
        落盘 {DATA_DIR}/assets/generated/<uuid><ext> → output 置 completed
                                        │
        浏览器轮询任务 → 画廊展示 /assets-runtime/generated/<file>
```

## 后端布局

入口 `backend/cmd/airgate-studio/main.go`（配置 → DB 迁移 → worker → HTTP → 优雅退出）；
全部实现在单包 `backend/internal/studio/`：

| 文件 | 职责 |
|---|---|
| `config.go` | 配置装载：config.yaml（CONFIG_PATH，可缺省）+ env 覆盖（必填缺失启动即错） |
| `db.go` | Postgres 连接 + 幂等迁移（studio_users / studio_tasks / studio_groups / studio_models / studio_user_keys） |
| `auth.go` | OAuth 登录/回调/登出、HMAC 会话 cookie、requireUser 中间件；回调按组 provision key（管理员自动收集分组镜像） |
| `users.go` | UserStore 接口 + pg 实现（api_key 明文只存库、永不出 API；含用户按组 key 表） |
| `shelf.go` | 分组×模型货架：ShelfStore 接口 + pg 实现（分组镜像/开关、按组模型同步/上架、漂移标记） |
| `admin.go` | 管理端接口（requireAdmin：config 白名单）：分组开关、按组同步模型、上架编辑 |
| `tasks.go` | Task 模型、TaskStore 接口 + pg 实现（领取/完成/失败重试状态机） |
| `worker.go` | 单 goroutine 轮询执行；`executeTask` 按模态（kind）分发 → `executeImageTask`；`generateImages` 为图像协议分发单一切换点 |
| `strategy.go` | 执行策略判定纯函数（resolveStrategy）+ 模态启发式（guessModalityByModelName）+ model→protocols 目录 TTL 缓存 |
| `genimage_openai.go` | /v1/images/generations|edits 请求构造（含 multipart）与响应归一 |
| `genimage_gemini.go` | :predict 参数映射（sampleCount/aspectRatio 等）与 :generateContent 装配/解析 |
| `genimage_chat.go` | chat/completions 多模态回退路径装配与响应图片提取 |
| `coreclient.go` | core HTTP 客户端（oauth 端点 + 生图端点 + models + usage） |
| `assets.go` | 本地磁盘资产存储、文件名白名单校验（防路径穿越）、data URI/MIME 工具 |
| `generation.go` | 请求归一化、task input 组装、task → 前端响应映射 |
| `routes.go` | Server 装配 + 全部 HTTP 处理器（ServeMux 路由） |
| `spa.go` | embed webdist 托管 SPA（fallback index.html） |

## 任务状态机

`pending → processing →（completed | 失败 attempts+1 →（< max_attempts 回 pending，否则 failed））`

- 领取：事务内 `SELECT ... WHERE status='pending' ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1`。
- 进程重启：worker 启动先把遗留 processing 重置回 pending（单实例假设）。
- 任务删除同步删产物文件（output 的 images/assets 去重合并后清理本存储管理的 URL）。

## 配置

装载顺序：默认值 → `config.yaml`（`CONFIG_PATH` 指定，默认工作目录 `config.yaml`，即 `backend/config.yaml`，缺省跳过；显式指定但文件不存在报错）→ env 覆盖单项。样例 `backend/config.yaml.example`；`config.yaml` 已 gitignore（含密钥）。

| yaml 键 | env | 必填 | 说明 |
|---|---|---|---|
| `listen_addr` | `LISTEN_ADDR` | 否 | 监听地址，默认 `:8181` |
| `database_url` | `DATABASE_URL` | 是 | Postgres 连接串 |
| `airgate_base_url` | `AIRGATE_BASE_URL` | 是 | core 地址（服务端调用） |
| `airgate_public_url` | `AIRGATE_PUBLIC_URL` | 否 | 浏览器可达的 core 地址（授权跳转），默认同上 |
| `oauth_client_id` / `oauth_client_secret` | `OAUTH_CLIENT_ID` / `OAUTH_CLIENT_SECRET` | 是 | core 侧登记的 OAuth 客户端 |
| `public_base_url` | `PUBLIC_BASE_URL` | 是 | 本应用对外地址（拼 redirect_uri） |
| `session_secret` | `SESSION_SECRET` | 是 | 会话 cookie HMAC 密钥 |
| `data_dir` | `DATA_DIR` | 否 | 数据目录，默认 `data`（产物在 `data/assets/generated/`） |
| `admin_airgate_user_ids` | `ADMIN_AIRGATE_USER_IDS`（逗号分隔） | 否 | studio 管理员的 core 用户 ID 白名单（分组开关/模型上架） |

## 🚫 红线

- 禁止引入 gin/echo 等 web 框架（stdlib ServeMux 已够用）、禁止依赖 `airgate-sdk`。
- 渠道调度/计费是 core 的职责；本服务只拿 sk- key 调 `/v1/...`，不感知渠道。
- `api_key` 明文永不出现在任何 API 响应（`/api/user/info` 只回 `api_key_ready` 布尔）。
- adaptor 演进：生成执行统一走 `worker.go` 的 `executeTask` 按模态（kind）分发——
  新模态（video/music…）加 `executeXxxTask` 分支 + `gen<kind>_*.go` 实现，并把 kind
  加入 `generation.go` 的 `supportedKinds` 白名单（创建入口拦截未支持模态）；图像内部
  按协议走 `generateImages` 策略分发，新增路径只加 `strategy.go` 判定分支 +
  `genimage_*.go` 实现。勿在 handler/store 层散落协议细节；判定规则保持纯函数 +
  表驱动测试。产物统一写 `output.assets`（`[{type,url}]`，images 保留兼容），任务
  删除按 assets 清理本地文件。货架模型有 `modality` 维度（同步启发式预填、管理员可改）。
- `/assets-runtime/generated/` 文件名必须过 `assetFileNamePattern` 白名单。
- 注释中文；`_test.go` 同包、表驱动；DB 交互经 Store 接口抽象（单测用内存实现）。

## 命令

```bash
make install   # 前后端依赖
make ci        # lint + vet + test + build（与 CI 一致）
make build     # 前端构建 → 嵌入 → 单二进制 bin/airgate-studio
make dev       # 并行启动：后端 go run（:8181，读 backend/config.yaml）+ 前端 vite dev（:5174）
cd backend && GOWORK=off go test ./internal/studio/ -v -count=1   # 后端单测
```

## 前端（`web/`）

- 标准 Vite SPA：入口 `index.html` → `src/main.tsx`（注入主题）→ `src/App.tsx`
  （拉 `/api/user/info`，401 由 `lib/api.ts` 统一跳 `/auth/login`；渲染
  `components/UserBar` 用户区 + `studio/StudioView`）。
- 目录：`src/lib/`（api 客户端、工具）、`src/components/`（UserBar / AdminPanel）、
  `src/studio/`（创作中心：StudioView / StudioContext / GalleryView / MaskEditor /
  ModelPicker / SizeSelector / modelConfig / inspirations / studioStyles / types）。
- `src/lib/api.ts` 同源 cookie，基路径 `/api`；401 统一跳登录。产物消费统一走任务响应的
  `images`（数组）/`assets`（`[{type,url}]`）。
- 界面文案直接写中文字面量，无 i18n 层。
- dev：`pnpm dev`（:5174，代理 `/api` `/auth` `/assets-runtime` → :8181）。
