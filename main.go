// Command gateway puts a password login page in front of an upstream web app
// and reverse-proxies (including WebSocket) authenticated requests to it.
package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"html/template"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	loginPath  = "/__gateway/login"
	logoutPath = "/__gateway/logout"
	cookieName = "gateway_session"
	sessionTTL = 7 * 24 * time.Hour
	failDelay  = time.Second
)

var loginPage = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="zh-Hant">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>登入</title>
<style>
  body { margin: 0; min-height: 100vh; display: flex; align-items: center; justify-content: center;
         font-family: system-ui, sans-serif; background: #f2f3f5; color: #1f2328; }
  form { background: #fff; padding: 32px; border-radius: 12px; width: min(320px, calc(100vw - 32px));
         box-sizing: border-box; box-shadow: 0 4px 24px rgba(0,0,0,.08); }
  h1 { margin: 0 0 20px; font-size: 20px; }
  input, button { width: 100%; box-sizing: border-box; padding: 10px 12px; font-size: 16px; border-radius: 8px; }
  input { border: 1px solid #d0d7de; margin-bottom: 12px; }
  button { border: 0; background: #2563eb; color: #fff; cursor: pointer; }
  .error { color: #cf222e; margin: 0 0 12px; font-size: 14px; }
  @media (prefers-color-scheme: dark) {
    body { background: #0d1117; color: #e6edf3; }
    form { background: #161b22; }
    input { background: #0d1117; color: #e6edf3; border-color: #30363d; }
  }
</style>
</head>
<body>
<form method="post" action="` + loginPath + `">
  <h1>請輸入密碼</h1>
  {{if .}}<p class="error">{{.}}</p>{{end}}
  <input type="password" name="password" autofocus required autocomplete="current-password">
  <button type="submit">登入</button>
</form>
</body>
</html>`))

type gateway struct {
	password []byte
	key      []byte
	proxy    *httputil.ReverseProxy
}

func newGateway(password string, target *url.URL) *gateway {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}
	return &gateway{
		password: []byte(password),
		key:      key,
		proxy: &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.SetXForwarded()
		}},
	}
}

// sign returns a token of the form "<unix expiry>.<base64 hmac>".
func (g *gateway) sign(expiry time.Time) string {
	exp := strconv.FormatInt(expiry.Unix(), 10)
	mac := hmac.New(sha256.New, g.key)
	mac.Write([]byte(exp))
	return exp + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (g *gateway) valid(token string) bool {
	exp, _, ok := strings.Cut(token, ".")
	if !ok {
		return false
	}
	unix, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || time.Now().Unix() > unix {
		return false
	}
	return hmac.Equal([]byte(token), []byte(g.sign(time.Unix(unix, 0))))
}

func (g *gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case loginPath:
		g.login(w, r)
	case logoutPath:
		http.SetCookie(w, &http.Cookie{Name: cookieName, Path: "/", MaxAge: -1})
		http.Redirect(w, r, loginPath, http.StatusFound)
	default:
		if c, err := r.Cookie(cookieName); err == nil && g.valid(c.Value) {
			g.proxy.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodGet && r.Header.Get("Upgrade") == "" {
			http.Redirect(w, r, loginPath, http.StatusFound)
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}
}

func (g *gateway) login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method != http.MethodPost {
		loginPage.Execute(w, nil)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.PostFormValue("password")), g.password) != 1 {
		time.Sleep(failDelay)
		w.WriteHeader(http.StatusUnauthorized)
		loginPage.Execute(w, "密碼錯誤")
		return
	}
	expiry := time.Now().Add(sessionTTL)
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    g.sign(expiry),
		Path:     "/",
		Expires:  expiry,
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func main() {
	password := os.Getenv("PASSWORD")
	target, err := url.Parse(os.Getenv("TARGET_URL"))
	if password == "" || err != nil || target.Scheme == "" || target.Host == "" {
		log.Fatal("PASSWORD and TARGET_URL (e.g. http://firefox:3000) must be set")
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("listening on :%s, proxying to %s", port, target)
	log.Fatal(http.ListenAndServe(":"+port, newGateway(password, target)))
}
