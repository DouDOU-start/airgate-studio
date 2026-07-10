package studio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"sort"
	"strings"
	"time"
)

// CoreClient 封装对 core（AirGate 网关）的服务端 HTTP 调用：
// OAuth 协议端点（token/userinfo/provision-key）+ OpenAI 兼容端点（chat/completions、models、usage）。
type CoreClient struct {
	baseURL string
	// hc 不设全局超时，调用方统一经 ctx 控制（图像生成可能长达数分钟）。
	hc *http.Client
}

// NewCoreClient 创建 core 客户端；baseURL 形如 http://localhost:8080。
func NewCoreClient(baseURL string) *CoreClient {
	return &CoreClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		hc:      &http.Client{},
	}
}

// ==================== OAuth 协议端点 ====================

// TokenResult /oauth/token 成功响应。
type TokenResult struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// oauthError RFC 6749 错误体。
type oauthError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// ExchangeToken 用授权码换取访问令牌（授权码 + PKCE）。
func (c *CoreClient) ExchangeToken(ctx context.Context, code, redirectURI, clientID, clientSecret, codeVerifier string) (*TokenResult, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code_verifier": {codeVerifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/oauth/token",
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	body, status, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("换取令牌失败: %s", oauthErrMessage(body, status))
	}
	var out TokenResult
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("解析令牌响应失败: %w", err)
	}
	if out.AccessToken == "" {
		return nil, fmt.Errorf("令牌响应缺少 access_token")
	}
	return &out, nil
}

// CoreUserInfo /oauth/userinfo 响应。
type CoreUserInfo struct {
	Sub   string `json:"sub"` // core user_id 的数字字符串
	Name  string `json:"name"`
	Email string `json:"email"`
}

// UserInfo 用访问令牌获取用户身份。
func (c *CoreClient) UserInfo(ctx context.Context, accessToken string) (*CoreUserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/oauth/userinfo", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	body, status, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("获取用户信息失败: %s", oauthErrMessage(body, status))
	}
	var out CoreUserInfo
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("解析用户信息失败: %w", err)
	}
	return &out, nil
}

// ProvisionKeyResult /oauth/provision-key 响应。
type ProvisionKeyResult struct {
	APIKey  string `json:"api_key"`
	KeyHint string `json:"key_hint"`
	Created bool   `json:"created"`
}

// ErrProvisionKeyForbidden 用户在 core 侧禁用了该应用的 API Key（403）。
var ErrProvisionKeyForbidden = fmt.Errorf("用户已禁用该应用的 API Key")

// ProvisionKey 为令牌对应用户领取（get-or-create）应用专属 sk- key；幂等。
func (c *CoreClient) ProvisionKey(ctx context.Context, accessToken string) (*ProvisionKeyResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/oauth/provision-key", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	body, status, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if status == http.StatusForbidden {
		return nil, ErrProvisionKeyForbidden
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("领取 API Key 失败: %s", oauthErrMessage(body, status))
	}
	var out ProvisionKeyResult
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("解析 API Key 响应失败: %w", err)
	}
	if out.APIKey == "" {
		return nil, fmt.Errorf("API Key 响应为空")
	}
	return &out, nil
}

// ==================== 生成端点（Bearer sk- key，零翻译透传上游） ====================

// filePart multipart 请求中的一个文件字段（/v1/images/edits 用）。
type filePart struct {
	Field string // 表单字段名（image / image[] / mask）
	Name  string // 文件名
	MIME  string // Content-Type
	Data  []byte
}

// ChatCompletions 调 core 的 POST /v1/chat/completions；返回解析后的响应 JSON。
// 上游错误（非 2xx）转换为携带上游错误信息的 error。
func (c *CoreClient) ChatCompletions(ctx context.Context, apiKey string, payload map[string]any) (map[string]any, error) {
	return c.postGenerationJSON(ctx, apiKey, "/v1/chat/completions", payload)
}

// ImagesGenerations 调 core 的 POST /v1/images/generations（OpenAI Images 文生图）。
// 注意该端点不支持 stream:true（core 会 400），payload 中不得携带 stream。
func (c *CoreClient) ImagesGenerations(ctx context.Context, apiKey string, payload map[string]any) (map[string]any, error) {
	return c.postGenerationJSON(ctx, apiKey, "/v1/images/generations", payload)
}

// GeminiGenerateContent 调 core 的 POST /v1beta/models/{model}:generateContent（Gemini 原生多模态）。
func (c *CoreClient) GeminiGenerateContent(ctx context.Context, apiKey, model string, payload map[string]any) (map[string]any, error) {
	return c.postGenerationJSON(ctx, apiKey, "/v1beta/models/"+url.PathEscape(model)+":generateContent", payload)
}

// GeminiPredict 调 core 的 POST /v1beta/models/{model}:predict（Imagen 系）。
func (c *CoreClient) GeminiPredict(ctx context.Context, apiKey, model string, payload map[string]any) (map[string]any, error) {
	return c.postGenerationJSON(ctx, apiKey, "/v1beta/models/"+url.PathEscape(model)+":predict", payload)
}

// ImagesEdits 调 core 的 POST /v1/images/edits（multipart/form-data 图生图）。
// core 对 multipart 原样透传（渠道 model_mapping/param_override 不生效）。
func (c *CoreClient) ImagesEdits(ctx context.Context, apiKey string, fields map[string]string, files []filePart) (map[string]any, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	// 字段按名排序写入，保证请求体可复现（便于调试与测试）
	names := make([]string, 0, len(fields))
	for k := range fields {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if err := mw.WriteField(k, fields[k]); err != nil {
			return nil, fmt.Errorf("写入表单字段失败: %w", err)
		}
	}
	for _, f := range files {
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, f.Field, f.Name))
		if f.MIME != "" {
			h.Set("Content-Type", f.MIME)
		}
		part, err := mw.CreatePart(h)
		if err != nil {
			return nil, fmt.Errorf("创建文件字段失败: %w", err)
		}
		if _, err := part.Write(f.Data); err != nil {
			return nil, fmt.Errorf("写入文件内容失败: %w", err)
		}
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("收尾 multipart 失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/images/edits", &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+apiKey)
	return c.decodeGenerationResponse(c.do(req))
}

// postGenerationJSON 生成类 JSON 端点的公共调用：序列化 → Bearer 鉴权 → 错误归一。
func (c *CoreClient) postGenerationJSON(ctx context.Context, apiKey, path string, payload map[string]any) (map[string]any, error) {
	buf, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	return c.decodeGenerationResponse(c.do(req))
}

// decodeGenerationResponse 生成端点响应的统一解码：非 2xx 提取上游错误信息。
// openaiErrMessage 同时兼容 OpenAI 与 Gemini 的 {"error":{"message":...}} 错误形态。
func (c *CoreClient) decodeGenerationResponse(body []byte, status int, err error) (map[string]any, error) {
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("生成请求失败: %s", openaiErrMessage(body, status))
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("解析生成响应失败: %w", err)
	}
	return out, nil
}

// ListModels 透传 core 的 GET /v1/models（body + 状态码原样返回给前端）。
func (c *CoreClient) ListModels(ctx context.Context, apiKey string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/models", nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	return c.do(req)
}

// ModelProtocols 拉取 core /v1/models，返回 model id → 支持协议（core 的非标 protocols 字段）。
func (c *CoreClient) ModelProtocols(ctx context.Context, apiKey string) (map[string][]string, error) {
	body, status, err := c.ListModels(ctx, apiKey)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("查询模型列表失败: HTTP %d", status)
	}
	var out struct {
		Data []struct {
			ID        string   `json:"id"`
			Protocols []string `json:"protocols"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("解析模型列表失败: %w", err)
	}
	byModel := make(map[string][]string, len(out.Data))
	for _, item := range out.Data {
		if item.ID != "" {
			byModel[item.ID] = item.Protocols
		}
	}
	return byModel, nil
}

// UserBalance 调 core 的 GET /v1/usage 查询该 key 的可用余额。
func (c *CoreClient) UserBalance(ctx context.Context, apiKey string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/usage", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	body, status, err := c.do(req)
	if err != nil {
		return nil, err
	}
	// /v1/usage 对无效 key 也返回结构化 JSON（is_active=false），非 200 才视为失败。
	if status != http.StatusOK {
		return nil, fmt.Errorf("查询余额失败: HTTP %d", status)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("解析余额响应失败: %w", err)
	}
	return out, nil
}

// FetchBinary 下载二进制资源（生成结果的远程图片 URL 落地本地用）。
// 返回 body 与 Content-Type；限制读取上限，避免异常上游拖垮内存。
func (c *CoreClient) FetchBinary(ctx context.Context, rawURL string, maxBytes int64) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return nil, "", err
	}
	return data, resp.Header.Get("Content-Type"), nil
}

// do 执行请求并整体读取 body；网络错误统一包装。
func (c *CoreClient) do(req *http.Request) ([]byte, int, error) {
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("请求 core 失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, 0, fmt.Errorf("读取 core 响应失败: %w", err)
	}
	return body, resp.StatusCode, nil
}

// oauthErrMessage 从 RFC 6749 错误体提取可读信息。
func oauthErrMessage(body []byte, status int) string {
	var e oauthError
	if err := json.Unmarshal(body, &e); err == nil && e.Error != "" {
		if e.ErrorDescription != "" {
			return fmt.Sprintf("%s (%s)", e.Error, e.ErrorDescription)
		}
		return e.Error
	}
	return fmt.Sprintf("HTTP %d", status)
}

// openaiErrMessage 从 OpenAI 形态错误体（{"error":{"message":...}}）提取可读信息。
func openaiErrMessage(body []byte, status int) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &e); err == nil && e.Error.Message != "" {
		return e.Error.Message
	}
	// 有些实现直接返回 {"error": "..."} 字符串
	var alt struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &alt); err == nil && alt.Error != "" {
		return alt.Error
	}
	return fmt.Sprintf("HTTP %d", status)
}

// defaultOAuthTimeout OAuth 协议端点调用超时（登录链路上的短请求）。
const defaultOAuthTimeout = 15 * time.Second
