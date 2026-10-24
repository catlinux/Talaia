package checks

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
)

func TestTimeoutRedirectAndTLS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/slow":
			time.Sleep(1500 * time.Millisecond)
		case "/3":
			http.Redirect(w, r, "/2", 302)
		case "/2":
			http.Redirect(w, r, "/1", 302)
		case "/1":
			http.Redirect(w, r, "/done", 302)
		case "/4":
			http.Redirect(w, r, "/3", 302)
		case "/done":
			w.WriteHeader(200)
		}
	}))
	defer server.Close()
	r := New(Options{Store: &testStore{private: true}})
	for _, tt := range []struct {
		path string
		cfg  string
		want string
		ok   bool
	}{{"/slow", `{"timeout_seconds":1}`, "timeout", false}, {"/3", `{}`, "", true}, {"/4", `{}`, "too many redirects", false}} {
		v := r.RunOnce(context.Background(), store.AlertCheck{ID: "test", Kind: "http", Target: server.URL + tt.path, Config: json.RawMessage(tt.cfg)})
		if v.Error != tt.want || v.OK != tt.ok {
			t.Errorf("%s result=%+v", tt.path, v)
		}
	}
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer tlsServer.Close()
	tr := r.RunOnce(context.Background(), store.AlertCheck{ID: "tls-http", Kind: "http", Target: tlsServer.URL, Config: json.RawMessage(`{}`)})
	if tr.Error != "tls: certificate verification failed" || tr.CertNotAfter == nil {
		t.Fatalf("https result=%+v", tr)
	}
	addr := tlsServer.Listener.Addr().String()
	tc := r.RunOnce(context.Background(), store.AlertCheck{ID: "tls", Kind: "tls", Target: addr})
	if tc.Error != "tls: certificate verification failed" || tc.CertNotAfter == nil {
		t.Fatalf("tls result=%+v", tc)
	}
}

func TestTCPChecks(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	go func() {
		c, e := ln.Accept()
		if e == nil {
			_ = c.Close()
		}
	}()
	r := New(Options{Store: &testStore{private: true}})
	ok := r.RunOnce(context.Background(), store.AlertCheck{ID: "tcp", Kind: "tcp", Target: addr})
	if !ok.OK {
		t.Fatalf("tcp result=%+v", ok)
	}
	_ = ln.Close()
	_, port, _ := net.SplitHostPort(addr)
	closed := "127.0.0.1:" + port
	bad := r.RunOnce(context.Background(), store.AlertCheck{ID: "refused", Kind: "tcp", Target: closed})
	if bad.OK || bad.Error != "connection refused" {
		t.Fatalf("refused result=%+v", bad)
	}
}
