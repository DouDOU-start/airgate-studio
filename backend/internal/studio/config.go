package studio

import (
	"fmt"
	"os"
	"strings"
)

// Config 独立部署所需的全部环境配置。
//
// 必填项缺失时 LoadConfig 直接报错，服务启动即失败，避免半配置状态运行。
type Config struct {
	// ListenAddr HTTP 监听地址，env LISTEN_ADDR，默认 ":8181"。
	ListenAddr string
	// DatabaseURL Postgres 连接串，env DATABASE_URL，必填。
	DatabaseURL string
	// AirgateBaseURL core 部署地址（服务端到服务端调用），env AIRGATE_BASE_URL，必填。
	AirgateBaseURL string
	// AirgatePublicURL 浏览器可达的 core 地址（OAuth 授权跳转用），
	// env AIRGATE_PUBLIC_URL，可选，默认同 AirgateBaseURL。
	AirgatePublicURL string
	// OAuthClientID / OAuthClientSecret core 侧登记的 OAuth 客户端凭据，必填。
	OAuthClientID     string
	OAuthClientSecret string
	// PublicBaseURL 本应用对外地址，用于拼 redirect_uri，env PUBLIC_BASE_URL，必填。
	PublicBaseURL string
	// SessionSecret 会话 cookie 的 HMAC 签名密钥，env SESSION_SECRET，必填。
	SessionSecret string
	// DataDir 本地数据目录（生成图片落盘），env DATA_DIR，默认 "data"。
	DataDir string
}

// LoadConfig 从环境变量装载配置；必填项缺失返回错误。
func LoadConfig() (*Config, error) {
	cfg := &Config{
		ListenAddr:        envOr("LISTEN_ADDR", ":8181"),
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		AirgateBaseURL:    trimURL(os.Getenv("AIRGATE_BASE_URL")),
		AirgatePublicURL:  trimURL(os.Getenv("AIRGATE_PUBLIC_URL")),
		OAuthClientID:     os.Getenv("OAUTH_CLIENT_ID"),
		OAuthClientSecret: os.Getenv("OAUTH_CLIENT_SECRET"),
		PublicBaseURL:     trimURL(os.Getenv("PUBLIC_BASE_URL")),
		SessionSecret:     os.Getenv("SESSION_SECRET"),
		DataDir:           envOr("DATA_DIR", "data"),
	}
	if cfg.AirgatePublicURL == "" {
		cfg.AirgatePublicURL = cfg.AirgateBaseURL
	}

	var missing []string
	for _, item := range []struct {
		name  string
		value string
	}{
		{"DATABASE_URL", cfg.DatabaseURL},
		{"AIRGATE_BASE_URL", cfg.AirgateBaseURL},
		{"OAUTH_CLIENT_ID", cfg.OAuthClientID},
		{"OAUTH_CLIENT_SECRET", cfg.OAuthClientSecret},
		{"PUBLIC_BASE_URL", cfg.PublicBaseURL},
		{"SESSION_SECRET", cfg.SessionSecret},
	} {
		if item.value == "" {
			missing = append(missing, item.name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("缺少必填环境变量: %s", strings.Join(missing, ", "))
	}
	return cfg, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func trimURL(v string) string {
	return strings.TrimRight(strings.TrimSpace(v), "/")
}
