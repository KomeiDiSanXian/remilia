// Package api 提供 Remilia 框架的 RESTful 管理 API。
//
// 架构概述：
//
//	api.Server 封装了 HTTP 服务器和路由注册，对外暴露一组 /api/v1/* 端点。
//	所有端点（除 /health 和 /version）均通过 Bearer Token 认证。
//	统一响应格式为 { code, message, data }。
//
// 端点分类：
//   - Bot 管理：/api/v1/bots/*（列表、详情、启停）
//   - 插件管理：/api/v1/plugins/*（列表、详情、启用/禁用/重载）
//   - 配置：/api/v1/config（当前配置快照，只读）
//   - 系统：/api/v1/health, /version, /stats
//
// 使用方式：
//
//	deps := api.Deps{Bot: bot, PluginMgr: pm, Registry: reg}
//	srv := api.NewServer(":9002", "api-key", deps)
//	srv.Start()
//	defer srv.Stop(ctx)
package api

import (
	"encoding/json"
	"net/http"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// APIResponse 是统一 API 响应结构。
// 所有端点均返回此格式：
//
//	成功：{ "code": 0,    "message": "ok",  "data": {...} }
//	失败：{ "code": 404,  "message": "...",  "data": null  }
type APIResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

// writeJSON 写入 JSON 响应，设置 Content-Type 和状态码。
func writeJSON(w http.ResponseWriter, status int, resp APIResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.WithError(err).Error("[API] Failed to encode JSON response")
	}
}

// writeOK 写入成功响应（code=0, status=200）。
func writeOK(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, APIResponse{Code: 0, Message: "ok", Data: data})
}

// writeErr 写入错误响应。
// code 为业务错误码，httpStatus 为 HTTP 状态码，msg 为人类可读的错误描述。
func writeErr(w http.ResponseWriter, code int, msg string, httpStatus int) {
	writeJSON(w, httpStatus, APIResponse{Code: code, Message: msg, Data: nil})
}
