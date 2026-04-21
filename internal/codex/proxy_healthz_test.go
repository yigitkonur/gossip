package codex

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProxyServeHTTP_HealthzReturns200(t *testing.T) {
	p := NewProxy(nil)
	srv := httptest.NewServer(p)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
}
