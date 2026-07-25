package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateConfigRejectsMissingPassword(t *testing.T) {
	if err := validateConfig(Config{}); err == nil {
		t.Fatal("expected an empty PANEL_PASSWORD to be rejected")
	}
	if err := validateConfig(Config{PanelPassword: "a-real-password"}); err != nil {
		t.Fatalf("expected configured password to pass validation: %v", err)
	}
}

func TestRequireAuthRejectsMissingAndInvalidSessions(t *testing.T) {
	app := &App{
		cfg:     Config{PanelPassword: "correct horse battery staple"},
		session: "expected-session-token",
	}
	protected := app.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	t.Run("missing cookie", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/state", nil)
		response := httptest.NewRecorder()
		protected.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", response.Code)
		}
	})

	t.Run("invalid cookie", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/state", nil)
		request.AddCookie(&http.Cookie{Name: "palctrl_session", Value: "wrong-token"})
		response := httptest.NewRecorder()
		protected.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", response.Code)
		}
	})

	t.Run("valid cookie", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/state", nil)
		request.AddCookie(&http.Cookie{Name: "palctrl_session", Value: "expected-session-token"})
		response := httptest.NewRecorder()
		protected.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d", response.Code)
		}
	})
}

func TestLoginRequiresCorrectPassword(t *testing.T) {
	app := &App{
		cfg:     Config{PanelPassword: "correct horse battery staple"},
		session: "new-session-token",
	}

	t.Run("wrong password", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"password":"wrong"}`))
		response := httptest.NewRecorder()
		app.handleLogin(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", response.Code)
		}
	})

	t.Run("correct password", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"password":"correct horse battery staple"}`))
		response := httptest.NewRecorder()
		app.handleLogin(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", response.Code)
		}
		cookies := response.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatalf("expected one session cookie, got %d", len(cookies))
		}
		if cookies[0].Name != "palctrl_session" || cookies[0].Value != "new-session-token" {
			t.Fatalf("unexpected session cookie: %#v", cookies[0])
		}
		if !cookies[0].HttpOnly {
			t.Fatal("expected the session cookie to be HttpOnly")
		}
	})
}
