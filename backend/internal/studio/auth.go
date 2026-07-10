package studio

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// OAuth2 授权码 + PKCE 单点登录，会话为 7 天 HMAC 签名 cookie。
//
// 流程：
//  1. GET /auth/login   生成 state + PKCE verifier，写短时签名 cookie，302 到 core 授权页；
//  2. GET /auth/callback 校验 state → 换 token → userinfo → provision-key →
//     upsert studio_users → 签发会话 cookie → 302 /。
const (
	sessionCookieName = "studio_session"
	oauthCookieName   = "studio_oauth"
	sessionTTL        = 7 * 24 * time.Hour
	oauthStateTTL     = 10 * time.Minute
)

// ==================== 签名工具 ====================

func (s *Server) sign(payload string) string {
	mac := hmac.New(sha256.New, []byte(s.cfg.SessionSecret))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Server) verifySig(payload, sig string) bool {
	return subtle.ConstantTimeCompare([]byte(s.sign(payload)), []byte(sig)) == 1
}

// signSession 生成会话 cookie 值：v1.<userID>.<expUnix>.<sig>。
func (s *Server) signSession(userID int64, exp time.Time) string {
	payload := fmt.Sprintf("v1.%d.%d", userID, exp.Unix())
	return payload + "." + s.sign(payload)
}

// parseSession 校验会话 cookie，返回本地用户 ID。
func (s *Server) parseSession(value string) (int64, bool) {
	parts := strings.Split(value, ".")
	if len(parts) != 4 || parts[0] != "v1" {
		return 0, false
	}
	payload := strings.Join(parts[:3], ".")
	if !s.verifySig(payload, parts[3]) {
		return 0, false
	}
	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return 0, false
	}
	userID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || userID <= 0 {
		return 0, false
	}
	return userID, true
}

// signOAuthState 生成 OAuth 中间态 cookie 值：<state>.<verifier>.<expUnix>.<sig>。
func (s *Server) signOAuthState(state, verifier string, exp time.Time) string {
	payload := fmt.Sprintf("%s.%s.%d", state, verifier, exp.Unix())
	return payload + "." + s.sign(payload)
}

// parseOAuthState 校验 OAuth 中间态 cookie，返回 (state, verifier)。
func (s *Server) parseOAuthState(value string) (state, verifier string, ok bool) {
	parts := strings.Split(value, ".")
	if len(parts) != 4 {
		return "", "", false
	}
	payload := strings.Join(parts[:3], ".")
	if !s.verifySig(payload, parts[3]) {
		return "", "", false
	}
	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func randomToken(nBytes int) string {
	buf := make([]byte, nBytes)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("crypto/rand 不可用: %v", err))
	}
	return hex.EncodeToString(buf)
}

// pkceChallenge 计算 S256 code_challenge = base64url(sha256(verifier))。
func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// cookieSecure PUBLIC_BASE_URL 为 https 时给 cookie 加 Secure。
func (s *Server) cookieSecure() bool {
	return strings.HasPrefix(s.cfg.PublicBaseURL, "https://")
}

func (s *Server) redirectURI() string {
	return s.cfg.PublicBaseURL + "/auth/callback"
}

// ==================== HTTP 处理器 ====================

// handleLogin GET /auth/login：生成 state+PKCE，302 到 core 授权页。
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	state := randomToken(16)
	verifier := randomToken(32) // 64 个 hex 字符，满足 RFC 7636 的 43-128 长度要求

	exp := time.Now().Add(oauthStateTTL)
	http.SetCookie(w, &http.Cookie{
		Name:     oauthCookieName,
		Value:    s.signOAuthState(state, verifier, exp),
		Path:     "/",
		MaxAge:   int(oauthStateTTL / time.Second),
		HttpOnly: true,
		Secure:   s.cookieSecure(),
		SameSite: http.SameSiteLaxMode,
	})

	q := url.Values{
		"client_id":             {s.cfg.OAuthClientID},
		"redirect_uri":          {s.redirectURI()},
		"state":                 {state},
		"code_challenge":        {pkceChallenge(verifier)},
		"code_challenge_method": {"S256"},
	}
	http.Redirect(w, r, s.cfg.AirgatePublicURL+"/oauth/authorize?"+q.Encode(), http.StatusFound)
}

// handleCallback GET /auth/callback：完成授权码换令牌与本地登录。
func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if errCode := query.Get("error"); errCode != "" {
		s.renderAuthError(w, fmt.Sprintf("授权被拒绝（%s）", errCode))
		return
	}
	code := query.Get("code")
	state := query.Get("state")
	if code == "" || state == "" {
		s.renderAuthError(w, "回调参数缺失")
		return
	}

	cookie, err := r.Cookie(oauthCookieName)
	if err != nil {
		s.renderAuthError(w, "登录会话已过期，请重新登录")
		return
	}
	// 一次性消费：无论成败都清掉中间态 cookie。
	http.SetCookie(w, &http.Cookie{Name: oauthCookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.cookieSecure(), SameSite: http.SameSiteLaxMode})

	wantState, verifier, ok := s.parseOAuthState(cookie.Value)
	if !ok || subtle.ConstantTimeCompare([]byte(wantState), []byte(state)) != 1 {
		s.renderAuthError(w, "state 校验失败，请重新登录")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), defaultOAuthTimeout)
	defer cancel()

	token, err := s.core.ExchangeToken(ctx, code, s.redirectURI(),
		s.cfg.OAuthClientID, s.cfg.OAuthClientSecret, verifier)
	if err != nil {
		s.logger.Error("oauth_exchange_token_failed", "error", err)
		s.renderAuthError(w, "换取令牌失败："+err.Error())
		return
	}

	info, err := s.core.UserInfo(ctx, token.AccessToken)
	if err != nil {
		s.logger.Error("oauth_userinfo_failed", "error", err)
		s.renderAuthError(w, "获取用户信息失败："+err.Error())
		return
	}
	airgateUserID, err := strconv.ParseInt(info.Sub, 10, 64)
	if err != nil || airgateUserID <= 0 {
		s.renderAuthError(w, "用户标识非法")
		return
	}

	// provision-key 幂等：已有 key 也会返回完整明文，覆盖存储保持最新。
	// 403（用户禁用了该 key）不阻断登录——先让用户进来，任务执行时再报可读错误。
	apiKey := ""
	if pk, err := s.core.ProvisionKey(ctx, token.AccessToken); err == nil {
		apiKey = pk.APIKey
	} else if errors.Is(err, ErrProvisionKeyForbidden) {
		s.logger.Warn("oauth_provision_key_forbidden", "airgate_user_id", airgateUserID)
	} else {
		s.logger.Error("oauth_provision_key_failed", "error", err)
		s.renderAuthError(w, "领取 API Key 失败："+err.Error())
		return
	}

	user, err := s.users.Upsert(r.Context(), airgateUserID, info.Email, info.Name, apiKey)
	if err != nil {
		s.logger.Error("upsert_user_failed", "error", err)
		s.renderAuthError(w, "写入本地用户失败")
		return
	}

	exp := time.Now().Add(sessionTTL)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    s.signSession(user.ID, exp),
		Path:     "/",
		MaxAge:   int(sessionTTL / time.Second),
		HttpOnly: true,
		Secure:   s.cookieSecure(),
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

// handleLogout POST /auth/logout：清除会话 cookie。
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cookieSecure(),
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// requireUser 会话鉴权中间件：从 cookie 解析用户并注入处理器；未登录返回 401。
func (s *Server) requireUser(next func(http.ResponseWriter, *http.Request, *User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		userID, ok := s.parseSession(cookie.Value)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		user, err := s.users.GetByID(r.Context(), userID)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r, user)
	}
}

// renderAuthError 登录链路错误页（非 API 场景，浏览器直达）。
func (s *Server) renderAuthError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	_, _ = fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>登录失败</title>
<div style="font-family:system-ui;max-width:480px;margin:20vh auto;text-align:center">
<h2>登录失败</h2><p>%s</p><p><a href="/auth/login">重新登录</a></p></div>`, html.EscapeString(msg))
}
