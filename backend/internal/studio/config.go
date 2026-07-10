package studio

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config 独立部署所需的全部配置。
//
// 装载顺序：默认值 → config.yaml（CONFIG_PATH 指定，默认 ./config.yaml，允许缺省）→
// 环境变量覆盖单项。必填项缺失时 LoadConfig 直接报错，服务启动即失败，避免半配置状态运行。
type Config struct {
	// ListenAddr HTTP 监听地址，yaml listen_addr / env LISTEN_ADDR，默认 ":8181"。
	ListenAddr string `yaml:"listen_addr"`
	// DatabaseURL Postgres 连接串，yaml database_url / env DATABASE_URL，必填。
	DatabaseURL string `yaml:"database_url"`
	// AirgateBaseURL core 部署地址（服务端到服务端调用），yaml airgate_base_url / env AIRGATE_BASE_URL，必填。
	AirgateBaseURL string `yaml:"airgate_base_url"`
	// AirgatePublicURL 浏览器可达的 core 地址（OAuth 授权跳转用），
	// yaml airgate_public_url / env AIRGATE_PUBLIC_URL，可选，默认同 AirgateBaseURL。
	AirgatePublicURL string `yaml:"airgate_public_url"`
	// OAuthClientID / OAuthClientSecret core 侧登记的 OAuth 客户端凭据，必填。
	OAuthClientID     string `yaml:"oauth_client_id"`
	OAuthClientSecret string `yaml:"oauth_client_secret"`
	// PublicBaseURL 本应用对外地址，用于拼 redirect_uri，yaml public_base_url / env PUBLIC_BASE_URL，必填。
	PublicBaseURL string `yaml:"public_base_url"`
	// SessionSecret 会话 cookie 的 HMAC 签名密钥，yaml session_secret / env SESSION_SECRET，必填。
	SessionSecret string `yaml:"session_secret"`
	// DataDir 本地数据目录（生成图片落盘），yaml data_dir / env DATA_DIR，默认 "data"。
	DataDir string `yaml:"data_dir"`
	// AdminAirgateUserIDs studio 管理员的 core 用户 ID 列表（分组开关/模型上架权限），
	// yaml admin_airgate_user_ids / env ADMIN_AIRGATE_USER_IDS（逗号分隔），可选。
	AdminAirgateUserIDs []int64 `yaml:"admin_airgate_user_ids"`
}

// IsAdmin 判断 core 用户是否为 studio 管理员。
func (c *Config) IsAdmin(airgateUserID int64) bool {
	for _, id := range c.AdminAirgateUserIDs {
		if id == airgateUserID {
			return true
		}
	}
	return false
}

// configPath 返回配置文件路径（env CONFIG_PATH 优先，默认 ./config.yaml）。
func configPath() string {
	if v := strings.TrimSpace(os.Getenv("CONFIG_PATH")); v != "" {
		return v
	}
	return "config.yaml"
}

// LoadConfig 装载配置：默认值 → config.yaml（缺省则跳过）→ 环境变量覆盖；必填项缺失返回错误。
func LoadConfig() (*Config, error) {
	cfg := &Config{
		ListenAddr: ":8181",
		DataDir:    "data",
	}

	path := configPath()
	if data, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("解析配置文件 %s 失败: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("读取配置文件 %s 失败: %w", path, err)
	} else if os.Getenv("CONFIG_PATH") != "" {
		// 显式指定的配置文件不存在视为配置错误；默认路径缺省则回退纯环境变量。
		return nil, fmt.Errorf("配置文件不存在: %s", path)
	}

	applyEnvOverrides(cfg)

	cfg.AirgateBaseURL = trimURL(cfg.AirgateBaseURL)
	cfg.AirgatePublicURL = trimURL(cfg.AirgatePublicURL)
	cfg.PublicBaseURL = trimURL(cfg.PublicBaseURL)
	if cfg.AirgatePublicURL == "" {
		cfg.AirgatePublicURL = cfg.AirgateBaseURL
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8181"
	}
	if cfg.DataDir == "" {
		cfg.DataDir = "data"
	}

	var missing []string
	for _, item := range []struct {
		name  string
		value string
	}{
		{"database_url / DATABASE_URL", cfg.DatabaseURL},
		{"airgate_base_url / AIRGATE_BASE_URL", cfg.AirgateBaseURL},
		{"oauth_client_id / OAUTH_CLIENT_ID", cfg.OAuthClientID},
		{"oauth_client_secret / OAUTH_CLIENT_SECRET", cfg.OAuthClientSecret},
		{"public_base_url / PUBLIC_BASE_URL", cfg.PublicBaseURL},
		{"session_secret / SESSION_SECRET", cfg.SessionSecret},
	} {
		if item.value == "" {
			missing = append(missing, item.name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("缺少必填配置项: %s", strings.Join(missing, ", "))
	}
	return cfg, nil
}

// applyEnvOverrides 环境变量覆盖配置文件中的同名项（容器部署习惯）。
func applyEnvOverrides(cfg *Config) {
	envStr("LISTEN_ADDR", &cfg.ListenAddr)
	envStr("DATABASE_URL", &cfg.DatabaseURL)
	envStr("AIRGATE_BASE_URL", &cfg.AirgateBaseURL)
	envStr("AIRGATE_PUBLIC_URL", &cfg.AirgatePublicURL)
	envStr("OAUTH_CLIENT_ID", &cfg.OAuthClientID)
	envStr("OAUTH_CLIENT_SECRET", &cfg.OAuthClientSecret)
	envStr("PUBLIC_BASE_URL", &cfg.PublicBaseURL)
	envStr("SESSION_SECRET", &cfg.SessionSecret)
	envStr("DATA_DIR", &cfg.DataDir)
	if raw := strings.TrimSpace(os.Getenv("ADMIN_AIRGATE_USER_IDS")); raw != "" {
		var ids []int64
		for _, part := range strings.Split(raw, ",") {
			id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
			if err == nil && id > 0 {
				ids = append(ids, id)
			}
		}
		cfg.AdminAirgateUserIDs = ids
	}
}

// envStr 环境变量非空时覆盖目标值。
func envStr(key string, dst *string) {
	if v := os.Getenv(key); v != "" {
		*dst = v
	}
}

func trimURL(v string) string {
	return strings.TrimRight(strings.TrimSpace(v), "/")
}
