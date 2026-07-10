package studio

import (
	"testing"
	"time"
)

func newAuthTestServer() *Server {
	return &Server{cfg: &Config{SessionSecret: "test-secret", PublicBaseURL: "http://localhost:8181"}}
}

func TestSessionSignAndParse(t *testing.T) {
	s := newAuthTestServer()

	value := s.signSession(42, time.Now().Add(time.Hour))
	userID, ok := s.parseSession(value)
	if !ok || userID != 42 {
		t.Fatalf("parseSession = (%d, %v), want (42, true)", userID, ok)
	}
}

func TestSessionParseRejects(t *testing.T) {
	s := newAuthTestServer()
	valid := s.signSession(42, time.Now().Add(time.Hour))
	expired := s.signSession(42, time.Now().Add(-time.Minute))

	other := &Server{cfg: &Config{SessionSecret: "another-secret"}}
	forged := other.signSession(42, time.Now().Add(time.Hour))

	tests := []struct {
		name  string
		value string
	}{
		{"篡改 payload", "v1.43." + valid[len("v1.42."):]},
		{"过期", expired},
		{"异 secret 伪造", forged},
		{"格式非法", "garbage"},
		{"空值", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := s.parseSession(tt.value); ok {
				t.Fatalf("parseSession(%q) 应失败", tt.value)
			}
		})
	}
}

func TestOAuthStateSignAndParse(t *testing.T) {
	s := newAuthTestServer()

	value := s.signOAuthState("state-1", "verifier-1", time.Now().Add(time.Minute))
	state, verifier, ok := s.parseOAuthState(value)
	if !ok || state != "state-1" || verifier != "verifier-1" {
		t.Fatalf("parseOAuthState = (%s, %s, %v)", state, verifier, ok)
	}

	// 过期后不可用
	expired := s.signOAuthState("state-2", "verifier-2", time.Now().Add(-time.Minute))
	if _, _, ok := s.parseOAuthState(expired); ok {
		t.Fatalf("过期 state 应失败")
	}
}

func TestPKCEChallenge(t *testing.T) {
	// RFC 7636 附录 B 的标准测试向量
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got := pkceChallenge(verifier); got != want {
		t.Fatalf("pkceChallenge = %s, want %s", got, want)
	}
}
