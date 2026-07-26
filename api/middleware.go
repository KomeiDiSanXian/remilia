package api

import (
	"crypto/subtle"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// isLoopbackRequest 判断请求是否来自本机回环地址。
//
// 用于"未配置 api_key 时仅允许本机访问"的降级策略：既保留本地开发的免鉴权便利，
// 又避免管理 API 在监听 0.0.0.0 时对整个网络裸奔。
func isLoopbackRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// corsOriginAllowed 判断跨域请求来源是否可信。
//
// 仅放行桌面端 WebView（tauri://）与本机前端（localhost / 回环地址 / *.localhost），
// 这覆盖了 Tauri 桌面端和本地 SPA 开发服务器这两个既有使用场景。
func corsOriginAllowed(origin string) bool {
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if u.Scheme == "tauri" {
		return true
	}
	host := u.Hostname()
	// Windows 上的 Tauri 使用 https://tauri.localhost
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// cors 包装一个 http.Handler，添加 CORS 头以支持跨域请求
// （桌面端 Tauri WebView 和独立前端 SPA 均需要）。
//
// 注意：这里不能使用 Access-Control-Allow-Origin: "*"。管理 API 可启停 Bot、
// 改写配置、读取含密钥的配置，通配放行意味着用户浏览任意恶意网页时，
// 该页面的 JS 都能跨域调用本机管理 API 并读取响应。
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 响应随 Origin 变化，避免缓存串用
		w.Header().Add("Vary", "Origin")
		if origin := r.Header.Get("Origin"); corsOriginAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// auth 返回一个包装了认证检查的 HTTP handler。
//
// 认证方式：Bearer Token，检查 Authorization 请求头。
//   - 若 Server.apiKey 为空，跳过认证（开发模式）
//   - 若请求头缺失或格式错误，返回 401
//   - 若 Token 不匹配，返回 401
//   - 认证通过后，委托给 next handler
//
// 使用 subtle.ConstantTimeCompare 防止计时侧信道攻击。
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.apiKey == "" {
			// 未配置 api_key 时只允许本机回环访问。
			// 管理 API 可启停 Bot、改写 config.yaml、增删插件与权限角色，
			// 默认配置又是 enabled:true + addr ":9002"（监听所有网卡），
			// 若继续无条件放行，等于把控制面向整个网络开放。
			if isLoopbackRequest(r) {
				next(w, r)
				return
			}
			writeErr(w, 401,
				"api.api_key is not configured; remote access to the admin API is refused (set api.api_key in config.yaml)",
				http.StatusUnauthorized)
			return
		}

		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			writeErr(w, 401, "missing or invalid Authorization header", http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(auth, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.apiKey)) != 1 {
			writeErr(w, 401, "invalid api key", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}
