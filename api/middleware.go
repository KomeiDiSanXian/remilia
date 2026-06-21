package api

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// cors 包装一个 http.Handler，添加 CORS 头以支持跨域请求
// （桌面端 Tauri WebView 和独立前端 SPA 均需要）。
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

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
			next(w, r)
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
