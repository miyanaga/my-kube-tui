package k8s

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// 偽の API サーバー（TLS・HTTP/2 対応）を立て、kubeconfig 経由で NewClient から繋ぐ。
// client-go は TLS の接続で golang.org/x/net/http2 を使うので、
// x/net を上げたときに通信の経路が壊れていないかをここで確かめる。
// HTTP/2 になるかは x/net と Go のツールチェーンの組み合わせで変わる
// （x/net v0.55.0 は Go 1.26 では HTTP/2、Go 1.27 では HTTP/1.1 になった）ので、
// 通信できることだけを確かめ、プロトコルはログに残すに留める。
func newFakeAPIServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var http2Requests atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/namespaces", func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 {
			http2Requests.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"kind":       "NamespaceList",
			"apiVersion": "v1",
			"items": []map[string]any{
				{"metadata": map[string]any{"name": "web"}},
				{"metadata": map[string]any{"name": "default"}},
			},
		})
	})
	mux.HandleFunc("/api/v1/namespaces/web/pods/app-1/log", func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 {
			http2Requests.Add(1)
		}
		if got := r.URL.Query().Get("container"); got != "app" {
			t.Errorf("container = %q, want app", got)
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "2026-01-02T03:04:05.000000000Z hello\n")
	})
	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv, &http2Requests
}

func writeKubeconfig(t *testing.T, server string) {
	t.Helper()
	cfg := `apiVersion: v1
kind: Config
clusters:
- name: fake
  cluster:
    server: ` + server + `
    insecure-skip-tls-verify: true
users:
- name: fake
  user:
    token: dummy
contexts:
- name: fake
  context:
    cluster: fake
    user: fake
current-context: fake
`
	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBECONFIG", path)
}

func TestClientAgainstFakeAPIServer(t *testing.T) {
	srv, http2Requests := newFakeAPIServer(t)
	writeKubeconfig(t, srv.URL)

	c, err := NewClient("")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c.ContextName != "fake" || c.ClusterName != "fake" {
		t.Errorf("context/cluster = %q/%q, want fake/fake", c.ContextName, c.ClusterName)
	}

	ctx := context.Background()
	names, err := c.Namespaces(ctx)
	if err != nil {
		t.Fatalf("Namespaces: %v", err)
	}
	if strings.Join(names, ",") != "default,web" {
		t.Errorf("Namespaces = %v, want [default web]", names)
	}

	rc, err := c.openLogs(ctx, &Pod{Namespace: "web", Name: "app-1"}, "app", LogOptions{TailLines: 10})
	if err != nil {
		t.Fatalf("openLogs: %v", err)
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read logs: %v", err)
	}
	if !strings.Contains(string(body), "hello") {
		t.Errorf("logs = %q, want to contain hello", body)
	}

	t.Logf("HTTP/2 で届いた要求: %d / 2", http2Requests.Load())
}
