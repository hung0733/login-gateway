package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const testPassword = "s3cret"

func newTestGateway(t *testing.T, upstream http.Handler) *httptest.Server {
	t.Helper()
	up := httptest.NewServer(upstream)
	t.Cleanup(up.Close)
	target, err := url.Parse(up.URL)
	if err != nil {
		t.Fatal(err)
	}
	gw := httptest.NewServer(newGateway(testPassword, target))
	t.Cleanup(gw.Close)
	return gw
}

func noRedirectClient() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

func login(t *testing.T, gw *httptest.Server, password string) *http.Response {
	t.Helper()
	resp, err := noRedirectClient().PostForm(gw.URL+loginPath, url.Values{"password": {password}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

func sessionCookie(t *testing.T, resp *http.Response) *http.Cookie {
	t.Helper()
	for _, c := range resp.Cookies() {
		if c.Name == cookieName && c.Value != "" {
			return c
		}
	}
	t.Fatal("session cookie not set")
	return nil
}

func upstreamOK() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "upstream %s", r.URL.Path)
	})
}

func TestUnauthenticatedGetRedirectsToLogin(t *testing.T) {
	gw := newTestGateway(t, upstreamOK())
	resp, err := noRedirectClient().Get(gw.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != loginPath {
		t.Fatalf("got %d %q, want 302 to %s", resp.StatusCode, resp.Header.Get("Location"), loginPath)
	}
}

func TestUnauthenticatedNonGetIsUnauthorized(t *testing.T) {
	gw := newTestGateway(t, upstreamOK())
	resp, err := http.Post(gw.URL+"/api", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", resp.StatusCode)
	}
}

func TestLoginPageRendersForm(t *testing.T) {
	gw := newTestGateway(t, upstreamOK())
	resp, err := http.Get(gw.URL + loginPath)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `name="password"`) {
		t.Fatalf("got %d, body missing password field", resp.StatusCode)
	}
}

func TestWrongPasswordIsRejected(t *testing.T) {
	gw := newTestGateway(t, upstreamOK())
	resp := login(t, gw, "wrong")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == cookieName && c.Value != "" {
			t.Fatal("session cookie must not be set on wrong password")
		}
	}
}

func TestLoginThenProxy(t *testing.T) {
	gw := newTestGateway(t, upstreamOK())
	resp := login(t, gw, testPassword)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/" {
		t.Fatalf("got %d %q, want 303 to /", resp.StatusCode, resp.Header.Get("Location"))
	}
	cookie := sessionCookie(t, resp)
	if !cookie.HttpOnly {
		t.Fatal("session cookie must be HttpOnly")
	}

	req, _ := http.NewRequest(http.MethodGet, gw.URL+"/foo", nil)
	req.AddCookie(cookie)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if string(body) != "upstream /foo" {
		t.Fatalf("got body %q, want proxied upstream response", body)
	}
}

func TestTamperedCookieIsRejected(t *testing.T) {
	gw := newTestGateway(t, upstreamOK())
	cookie := sessionCookie(t, login(t, gw, testPassword))
	cookie.Value = cookie.Value[:len(cookie.Value)-1] + "x"

	req, _ := http.NewRequest(http.MethodGet, gw.URL+"/", nil)
	req.AddCookie(cookie)
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("got %d, want 302 to login", resp.StatusCode)
	}
}

func TestExpiredSessionIsRejected(t *testing.T) {
	g := newGateway(testPassword, &url.URL{Scheme: "http", Host: "unused"})
	token := g.sign(time.Now().Add(-time.Minute))
	if g.valid(token) {
		t.Fatal("expired token must be invalid")
	}
	if !g.valid(g.sign(time.Now().Add(time.Minute))) {
		t.Fatal("fresh token must be valid")
	}
}

func TestLogoutClearsCookie(t *testing.T) {
	gw := newTestGateway(t, upstreamOK())
	resp, err := noRedirectClient().Get(gw.URL + logoutPath)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	var cleared bool
	for _, c := range resp.Cookies() {
		if c.Name == cookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared || resp.Header.Get("Location") != loginPath {
		t.Fatal("logout must clear cookie and redirect to login")
	}
}

func TestWebSocketUpgradeIsProxied(t *testing.T) {
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "websocket" {
			http.Error(w, "expected upgrade", http.StatusBadRequest)
			return
		}
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		buf.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		buf.Flush()
		line, _ := buf.ReadString('\n')
		buf.WriteString("echo:" + line)
		buf.Flush()
	})
	gw := newTestGateway(t, upstream)
	cookie := sessionCookie(t, login(t, gw, testPassword))

	conn, err := net.Dial("tcp", strings.TrimPrefix(gw.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "GET /ws HTTP/1.1\r\nHost: x\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nCookie: %s=%s\r\n\r\n", cookie.Name, cookie.Value)

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("got %d, want 101", resp.StatusCode)
	}
	fmt.Fprint(conn, "hello\n")
	line, _ := reader.ReadString('\n')
	if line != "echo:hello\n" {
		t.Fatalf("got %q, want echo over upgraded connection", line)
	}
}
