package studio

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// auth_flow_test.go：OAuth 登录/回调链路的 HTTP 级集成测试。
// 用 httptest 打桩 core 的 /oauth/token、/oauth/userinfo、/oauth/provision-key，
// UserStore 复用 memUserStore，驱动 handleLogin → handleCallback 全链路。

// ── fake core：OAuth 三端点打桩 ──

type fakeOAuthCoreOptions struct {
	tokenStatus     int    // 非 0 时 /oauth/token 直接返回该状态码的 RFC6749 错误
	provisionStatus int    // 非 0 时 /oauth/provision-key 直接返回该状态码
	expectVerifier  string // 非空时校验 code_verifier 的 S256 结果等于该 challenge
}

type fakeOAuthCore struct {
	server *httptest.Server

	mu         sync.Mutex
	opts       fakeOAuthCoreOptions
	tokenCalls int
	lastToken  url.Values // /oauth/token 收到的表单
}

func newFakeOAuthCore(t *testing.T, opts fakeOAuthCoreOptions) *fakeOAuthCore {
	t.Helper()
	fc := &fakeOAuthCore{opts: opts}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth/token", fc.handleToken)
	mux.HandleFunc("GET /oauth/userinfo", fc.handleUserInfo)
	mux.HandleFunc("POST /oauth/provision-key", fc.handleProvisionKey)
	fc.server = httptest.NewServer(mux)
	t.Cleanup(fc.server.Close)
	return fc
}

// setExpectVerifier 登录拿到 challenge 后再设置，供 /oauth/token 校验 PKCE。
func (f *fakeOAuthCore) setExpectVerifier(challenge string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opts.expectVerifier = challenge
}

func (f *fakeOAuthCore) tokenForm() (url.Values, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastToken, f.tokenCalls
}

func (f *fakeOAuthCore) handleToken(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	f.mu.Lock()
	f.tokenCalls++
	f.lastToken = r.PostForm
	opts := f.opts
	f.mu.Unlock()

	if opts.tokenStatus != 0 {
		writeOAuthTestError(w, opts.tokenStatus, "invalid_grant", "code 无效或已使用")
		return
	}
	if opts.expectVerifier != "" {
		if pkceChallenge(r.PostFormValue("code_verifier")) != opts.expectVerifier {
			writeOAuthTestError(w, http.StatusBadRequest, "invalid_grant", "PKCE 校验失败")
			return
		}
	}
	writeTestJSON(w, http.StatusOK, map[string]any{
		"access_token": "oat_test_token",
		"token_type":   "Bearer",
		"expires_in":   7200,
	})
}

func (f *fakeOAuthCore) handleUserInfo(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer oat_test_token" {
		writeOAuthTestError(w, http.StatusUnauthorized, "invalid_token", "令牌无效")
		return
	}
	writeTestJSON(w, http.StatusOK, map[string]any{
		"sub":   "42",
		"name":  "测试用户",
		"email": "user@example.com",
		"groups": []map[string]any{
			{"id": 1, "name": "default", "rate_multiplier": 1},
			{"id": 2, "name": "vip", "rate_multiplier": 2, "note": "官转"},
		},
	})
}

// handleProvisionKey 模拟 core 的按组 provision：group_id=0 解析为默认分组 1，
// key 按分组区分（幂等键含分组）。
func (f *fakeOAuthCore) handleProvisionKey(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer oat_test_token" {
		writeOAuthTestError(w, http.StatusUnauthorized, "invalid_token", "令牌无效")
		return
	}
	f.mu.Lock()
	status := f.opts.provisionStatus
	f.mu.Unlock()
	if status != 0 {
		writeOAuthTestError(w, status, "access_denied", "key 已被禁用")
		return
	}
	var req struct {
		GroupID int64 `json:"group_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.GroupID == 0 {
		req.GroupID = 1
	}
	writeTestJSON(w, http.StatusOK, map[string]any{
		"api_key":  fmt.Sprintf("sk-test-provisioned-g%d", req.GroupID),
		"key_hint": "oned",
		"group_id": req.GroupID,
		"created":  true,
	})
}

func writeTestJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeOAuthTestError(w http.ResponseWriter, status int, code, desc string) {
	writeTestJSON(w, status, map[string]string{"error": code, "error_description": desc})
}

// ── 测试装配 ──

func newFlowTestServer(t *testing.T, coreURL string) (*Server, *memUserStore) {
	t.Helper()
	users := newMemUserStore()
	cfg := &Config{
		AirgateBaseURL:    coreURL,
		AirgatePublicURL:  coreURL,
		OAuthClientID:     "ac_test",
		OAuthClientSecret: "acs_test",
		PublicBaseURL:     "http://app.example",
		SessionSecret:     "test-secret",
	}
	return NewServer(cfg, nil, users, newMemShelfStore(), nil, NewCoreClient(coreURL), slog.New(slog.DiscardHandler)), users
}

// doLogin 执行 /auth/login，返回授权跳转 query 与 studio_oauth cookie。
func doLogin(t *testing.T, s *Server) (url.Values, *http.Cookie) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleLogin(rec, httptest.NewRequest(http.MethodGet, "/auth/login", nil))

	if rec.Code != http.StatusFound {
		t.Fatalf("login status = %d, want 302", rec.Code)
	}
	location, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("解析授权跳转 URL 失败: %v", err)
	}
	if location.Path != "/oauth/authorize" {
		t.Fatalf("授权跳转 path = %q, want /oauth/authorize", location.Path)
	}
	var oauthCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == oauthCookieName {
			oauthCookie = c
		}
	}
	if oauthCookie == nil || oauthCookie.Value == "" {
		t.Fatal("login 未签发 studio_oauth cookie")
	}
	return location.Query(), oauthCookie
}

// doCallback 携带 studio_oauth cookie 执行 /auth/callback。
func doCallback(t *testing.T, s *Server, rawQuery string, oauthCookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?"+rawQuery, nil)
	if oauthCookie != nil {
		req.AddCookie(oauthCookie)
	}
	rec := httptest.NewRecorder()
	s.handleCallback(rec, req)
	return rec
}

// sessionCookie 从响应中取出会话 cookie；不存在返回 nil。
func sessionCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName && c.MaxAge > 0 {
			return c
		}
	}
	return nil
}

// ── 用例 ──

// TestLoginRedirectParams 登录跳转必须携带 PKCE S256 challenge 与完整 OAuth 参数。
func TestLoginRedirectParams(t *testing.T) {
	t.Parallel()

	core := newFakeOAuthCore(t, fakeOAuthCoreOptions{})
	s, _ := newFlowTestServer(t, core.server.URL)
	query, _ := doLogin(t, s)

	if got := query.Get("client_id"); got != "ac_test" {
		t.Fatalf("client_id = %q, want ac_test", got)
	}
	if got := query.Get("redirect_uri"); got != "http://app.example/auth/callback" {
		t.Fatalf("redirect_uri = %q", got)
	}
	if query.Get("state") == "" || query.Get("code_challenge") == "" {
		t.Fatalf("state/code_challenge 缺失: %v", query)
	}
	if got := query.Get("code_challenge_method"); got != "S256" {
		t.Fatalf("code_challenge_method = %q, want S256", got)
	}
}

// TestCallbackHappyPath 全链路：换 token（含 PKCE verifier 校验）→ userinfo →
// provision-key → upsert 用户（api_key 落库）→ 签发本地用户会话 → 302 /。
func TestCallbackHappyPath(t *testing.T) {
	t.Parallel()

	core := newFakeOAuthCore(t, fakeOAuthCoreOptions{})
	s, users := newFlowTestServer(t, core.server.URL)

	query, oauthCookie := doLogin(t, s)
	// fake core 用登录时下发的 challenge 校验回调换 token 带的 verifier。
	core.setExpectVerifier(query.Get("code_challenge"))

	rec := doCallback(t, s, "code=oc_test&state="+query.Get("state"), oauthCookie)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
		t.Fatalf("callback = (%d, %q), want (302, /)；body: %s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}

	// /oauth/token 收到的表单参数完整。
	tokenForm, _ := core.tokenForm()
	if tokenForm == nil {
		t.Fatal("/oauth/token 未被调用")
	}
	for key, want := range map[string]string{
		"grant_type":    "authorization_code",
		"code":          "oc_test",
		"redirect_uri":  "http://app.example/auth/callback",
		"client_id":     "ac_test",
		"client_secret": "acs_test",
	} {
		if got := tokenForm.Get(key); got != want {
			t.Fatalf("/oauth/token 表单 %s = %q, want %q", key, got, want)
		}
	}

	// 用户已入库且默认分组 sk- key 落库；按组 key 写入 studio_user_keys。
	user, err := users.GetByID(context.Background(), 1)
	if err != nil {
		t.Fatalf("用户未入库: %v", err)
	}
	if user.AirgateUserID != 42 || user.APIKey != "sk-test-provisioned-g1" {
		t.Fatalf("用户 = %+v, want (airgate_user_id=42, api_key=sk-test-provisioned-g1)", user)
	}
	keys, err := users.KeysByUser(context.Background(), user.ID)
	if err != nil || keys[1] != "sk-test-provisioned-g1" {
		t.Fatalf("按组 key = %v, want group1 key", keys)
	}
	// 普通用户：未开放任何分组时只领默认组 key，不为其他可用分组领取。
	if _, ok := keys[2]; ok {
		t.Fatalf("未开放分组不应领 key: %v", keys)
	}

	// 会话 cookie 指向本地用户主键；过程态 cookie 已清除。
	session := sessionCookie(rec)
	if session == nil {
		t.Fatal("未签发会话 cookie")
	}
	userID, ok := s.parseSession(session.Value)
	if !ok || userID != user.ID {
		t.Fatalf("会话校验 = (%d, %v), want (%d, true)", userID, ok, user.ID)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == oauthCookieName && c.MaxAge != -1 {
			t.Fatalf("过程态 cookie 未清除: %+v", c)
		}
	}
}

// TestCallbackProvisionsEnabledGroupKeys 已开放分组 ∩ 用户可用分组 → 登录时按组补领 key。
func TestCallbackProvisionsEnabledGroupKeys(t *testing.T) {
	t.Parallel()

	core := newFakeOAuthCore(t, fakeOAuthCoreOptions{})
	s, users := newFlowTestServer(t, core.server.URL)
	// 管理员已开放分组 2（vip）。
	if err := s.shelf.UpsertGroups(context.Background(), []ShelfGroup{{CoreGroupID: 2, Name: "vip"}}); err != nil {
		t.Fatalf("准备分组失败: %v", err)
	}
	if err := s.shelf.SetGroupEnabled(context.Background(), 2, true); err != nil {
		t.Fatalf("开放分组失败: %v", err)
	}

	query, oauthCookie := doLogin(t, s)
	rec := doCallback(t, s, "code=oc_test&state="+query.Get("state"), oauthCookie)
	if rec.Code != http.StatusFound {
		t.Fatalf("callback = (%d, %s)", rec.Code, rec.Body.String())
	}

	keys, _ := users.KeysByUser(context.Background(), 1)
	if keys[1] != "sk-test-provisioned-g1" || keys[2] != "sk-test-provisioned-g2" {
		t.Fatalf("按组 key = %v, want group1 + group2", keys)
	}
}

// TestCallbackAdminCollectsGroups 管理员登录：自动收集分组镜像 + 为全部可用分组领 key。
func TestCallbackAdminCollectsGroups(t *testing.T) {
	t.Parallel()

	core := newFakeOAuthCore(t, fakeOAuthCoreOptions{})
	s, users := newFlowTestServer(t, core.server.URL)
	s.cfg.AdminAirgateUserIDs = []int64{42}

	query, oauthCookie := doLogin(t, s)
	rec := doCallback(t, s, "code=oc_test&state="+query.Get("state"), oauthCookie)
	if rec.Code != http.StatusFound {
		t.Fatalf("callback = (%d, %s)", rec.Code, rec.Body.String())
	}

	// 分组镜像已收集（默认不开放）。
	groups, err := s.shelf.ListGroups(context.Background(), false)
	if err != nil || len(groups) != 2 {
		t.Fatalf("分组镜像 = %v, %v, want 2 组", groups, err)
	}
	for _, g := range groups {
		if g.Enabled {
			t.Fatalf("自动收集的分组不应默认开放: %+v", g)
		}
	}
	// 管理员为全部可用分组领 key（同步模型要用）。
	keys, _ := users.KeysByUser(context.Background(), 1)
	if keys[1] == "" || keys[2] == "" {
		t.Fatalf("管理员按组 key = %v, want 两组齐备", keys)
	}
}

// TestCallbackRejections 异常分支：授权拒绝 / 参数缺失 / 无过程态 / state 不匹配。
func TestCallbackRejections(t *testing.T) {
	t.Parallel()

	core := newFakeOAuthCore(t, fakeOAuthCoreOptions{})
	s, _ := newFlowTestServer(t, core.server.URL)
	query, oauthCookie := doLogin(t, s)

	cases := []struct {
		name     string
		rawQuery string
		cookie   *http.Cookie
		wantMsg  string
	}{
		{name: "授权被拒绝", rawQuery: "error=access_denied", cookie: oauthCookie, wantMsg: "授权被拒绝"},
		{name: "缺少 code", rawQuery: "state=" + query.Get("state"), cookie: oauthCookie, wantMsg: "回调参数缺失"},
		{name: "无过程态 cookie", rawQuery: "code=oc_test&state=" + query.Get("state"), cookie: nil, wantMsg: "登录会话已过期"},
		{name: "state 不匹配", rawQuery: "code=oc_test&state=forged", cookie: oauthCookie, wantMsg: "state 校验失败"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doCallback(t, s, tc.rawQuery, tc.cookie)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tc.wantMsg) {
				t.Fatalf("body = %q, want 含 %q", rec.Body.String(), tc.wantMsg)
			}
			if sessionCookie(rec) != nil {
				t.Fatal("失败分支不应签发会话 cookie")
			}
		})
	}
	// 上述失败分支不应触达 /oauth/token。
	if _, calls := core.tokenForm(); calls != 0 {
		t.Fatalf("tokenCalls = %d, want 0", calls)
	}
}

// TestCallbackTokenExchangeFailure 换 token 失败（如授权码已用）时报错且不签发会话。
func TestCallbackTokenExchangeFailure(t *testing.T) {
	t.Parallel()

	core := newFakeOAuthCore(t, fakeOAuthCoreOptions{tokenStatus: http.StatusBadRequest})
	s, users := newFlowTestServer(t, core.server.URL)
	query, oauthCookie := doLogin(t, s)

	rec := doCallback(t, s, "code=oc_used&state="+query.Get("state"), oauthCookie)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "换取令牌失败") {
		t.Fatalf("callback = (%d, %q), want 403 + 换取令牌失败", rec.Code, rec.Body.String())
	}
	if sessionCookie(rec) != nil {
		t.Fatal("失败分支不应签发会话 cookie")
	}
	if _, err := users.GetByID(context.Background(), 1); err == nil {
		t.Fatal("失败分支不应写入用户")
	}
}

// TestCallbackProvisionKeyForbiddenAllowsLogin studio 差异化行为：
// provision-key 403（用户禁用 key）不阻断登录，api_key 存空串，任务执行时再报错。
func TestCallbackProvisionKeyForbiddenAllowsLogin(t *testing.T) {
	t.Parallel()

	core := newFakeOAuthCore(t, fakeOAuthCoreOptions{provisionStatus: http.StatusForbidden})
	s, users := newFlowTestServer(t, core.server.URL)
	query, oauthCookie := doLogin(t, s)

	rec := doCallback(t, s, "code=oc_test&state="+query.Get("state"), oauthCookie)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
		t.Fatalf("callback = (%d, %q), want 放行登录 (302, /)", rec.Code, rec.Header().Get("Location"))
	}
	if sessionCookie(rec) == nil {
		t.Fatal("403 放行分支应签发会话 cookie")
	}
	user, err := users.GetByID(context.Background(), 1)
	if err != nil {
		t.Fatalf("用户未入库: %v", err)
	}
	if user.APIKey != "" {
		t.Fatalf("api_key = %q, want 空串（key 被禁用）", user.APIKey)
	}
}
