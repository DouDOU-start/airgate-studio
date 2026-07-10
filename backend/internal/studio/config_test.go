package studio

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// configEnvKeys LoadConfig 涉及的全部环境变量；测试逐个置空避免宿主环境泄漏。
var configEnvKeys = []string{
	"CONFIG_PATH", "LISTEN_ADDR", "DATABASE_URL",
	"AIRGATE_BASE_URL", "AIRGATE_PUBLIC_URL",
	"OAUTH_CLIENT_ID", "OAUTH_CLIENT_SECRET",
	"PUBLIC_BASE_URL", "SESSION_SECRET", "DATA_DIR",
}

func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range configEnvKeys {
		t.Setenv(key, "")
	}
}

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写配置文件失败: %v", err)
	}
	return path
}

const validYAML = `
database_url: postgres://dev:dev@localhost:5432/studio?sslmode=disable
airgate_base_url: http://localhost:9517/
airgate_public_url: http://172.17.196.241:3000
oauth_client_id: ac_yaml
oauth_client_secret: acs_yaml
public_base_url: http://172.17.196.241:5174/
session_secret: yaml-secret
`

// TestLoadConfigFromYAML config.yaml 装载 + URL 去尾斜杠 + 默认值补齐。
func TestLoadConfigFromYAML(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("CONFIG_PATH", writeConfigFile(t, validYAML))

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig 失败: %v", err)
	}
	if cfg.OAuthClientID != "ac_yaml" || cfg.SessionSecret != "yaml-secret" {
		t.Fatalf("yaml 字段未装载: %+v", cfg)
	}
	if cfg.AirgateBaseURL != "http://localhost:9517" || cfg.PublicBaseURL != "http://172.17.196.241:5174" {
		t.Fatalf("URL 未去尾斜杠: %+v", cfg)
	}
	if cfg.ListenAddr != ":8181" || cfg.DataDir != "data" {
		t.Fatalf("默认值未补齐: %+v", cfg)
	}
	if cfg.AirgatePublicURL != "http://172.17.196.241:3000" {
		t.Fatalf("airgate_public_url = %q", cfg.AirgatePublicURL)
	}
}

// TestLoadConfigEnvOverridesYAML 环境变量覆盖 config.yaml 同名项。
func TestLoadConfigEnvOverridesYAML(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("CONFIG_PATH", writeConfigFile(t, validYAML))
	t.Setenv("OAUTH_CLIENT_ID", "ac_env")
	t.Setenv("LISTEN_ADDR", ":9999")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig 失败: %v", err)
	}
	if cfg.OAuthClientID != "ac_env" || cfg.ListenAddr != ":9999" {
		t.Fatalf("env 未覆盖 yaml: %+v", cfg)
	}
	if cfg.OAuthClientSecret != "acs_yaml" {
		t.Fatalf("未被 env 覆盖的字段应保留 yaml 值: %+v", cfg)
	}
}

// TestLoadConfigEnvOnly 无 config.yaml（默认路径缺省）时回退纯环境变量，行为向后兼容。
func TestLoadConfigEnvOnly(t *testing.T) {
	clearConfigEnv(t)
	t.Chdir(t.TempDir()) // 确保默认路径 ./config.yaml 不存在
	t.Setenv("DATABASE_URL", "postgres://dev:dev@localhost:5432/studio")
	t.Setenv("AIRGATE_BASE_URL", "http://localhost:9517")
	t.Setenv("OAUTH_CLIENT_ID", "ac_env")
	t.Setenv("OAUTH_CLIENT_SECRET", "acs_env")
	t.Setenv("PUBLIC_BASE_URL", "http://localhost:5174")
	t.Setenv("SESSION_SECRET", "env-secret")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig 失败: %v", err)
	}
	if cfg.OAuthClientID != "ac_env" || cfg.AirgatePublicURL != "http://localhost:9517" {
		t.Fatalf("纯 env 装载异常: %+v", cfg)
	}
}

// TestLoadConfigMissingRequired 必填项缺失时报错并列出缺项。
func TestLoadConfigMissingRequired(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("CONFIG_PATH", writeConfigFile(t, "listen_addr: \":8181\"\n"))

	_, err := LoadConfig()
	if err == nil {
		t.Fatal("必填项缺失应报错")
	}
	for _, want := range []string{"DATABASE_URL", "SESSION_SECRET"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("错误信息应含 %s: %v", want, err)
		}
	}
}

// TestLoadConfigExplicitPathMissing 显式 CONFIG_PATH 指向不存在的文件视为配置错误。
func TestLoadConfigExplicitPathMissing(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("CONFIG_PATH", filepath.Join(t.TempDir(), "nope.yaml"))

	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "配置文件不存在") {
		t.Fatalf("err = %v, want 配置文件不存在", err)
	}
}

// TestLoadConfigInvalidYAML 配置文件格式非法直接报错。
func TestLoadConfigInvalidYAML(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("CONFIG_PATH", writeConfigFile(t, "database_url: [broken"))

	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "解析配置文件") {
		t.Fatalf("err = %v, want 解析配置文件失败", err)
	}
}
