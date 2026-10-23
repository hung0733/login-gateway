# login-gateway

一個好細（約 7MB）嘅 Docker image，喺任何 web app 前面加一個**密碼登入頁**（唔係 Basic Auth）。登入之後會自動 reverse proxy 去 `TARGET_URL`，WebSocket 都支援，所以 `lscr.io/linuxserver/firefox` 呢類串流 app 用得。

## 環境變數

| 變數 | 必填 | 說明 |
|---|---|---|
| `PASSWORD` | 係 | 登入密碼 |
| `TARGET_URL` | 係 | 上游地址，例如 `http://firefox:3000` |
| `PORT` | 唔係 | Gateway listen 嘅 port，預設 `8080` |

## 快速開始

```bash
./setup.sh
```

Script 會逐項問你 container 名、上游 URL、docker network、port、要唔要 publish 出 host，同埋密碼，然後 build image 同開 container。如果同名 container 已經存在，會先問你係咪要換。

## 配合 Nginx Proxy Manager

1. Gateway、Firefox、NPM 放喺同一個 docker network
2. NPM Proxy Host 指去 `http://<container 名>:<PORT>`，**記得剔 "Websockets Support"**
3. Firefox container **唔好 publish port** 出 host，否則可以繞過登入
4. Firefox 嘅 `CUSTOM_USER` / `PASSWORD` 可以唔設，免得要登入兩次
5. `TARGET_URL` 用 Firefox 嘅 HTTP port `3000`（`3001` 係自簽 HTTPS，gateway 唔會接受）

## 行為

- 登入相關路徑：`/__gateway/login`、`/__gateway/logout`
- Session cookie 有效 7 日，HttpOnly、SameSite=Lax；經 HTTPS（`X-Forwarded-Proto: https`）時會加 Secure
- 簽 cookie 嘅 key 每次啟動隨機生成，即係 container restart 之後要重新登入
- 密碼錯會延遲 1 秒先回應，減慢暴力破解

## 開發

```bash
go test ./...
```
