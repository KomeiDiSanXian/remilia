package engine

import (
	stdctx "context"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// Option Engine 配置选项函数类型
type Option func(*Engine)

// MatcherOption Matcher 配置选项函数类型
type MatcherOption func(*Matcher)

// Middleware 中间件函数类型
type Middleware = context.Middleware

// MatcherCoordinator defines the operations Matcher needs from Engine.
type MatcherCoordinator interface {
	DeleteMatcher(m *Matcher)
	RebuildMatcherChain(m *Matcher)
	InvalidateSortedCache(eventType dto.EventType)
	UpdateTempMatcherPriority(m *Matcher)
	UpdateMatcherCommand(m *Matcher)
	MigrateMatcherToTemp(m *Matcher)
	MigrateMatcherFromTemp(m *Matcher)
}

// Adapter connects an event source to the Bot
type Adapter interface {
	Start(ctx stdctx.Context, handleFunc func(*dto.Payload)) error
	Stop(ctx stdctx.Context) error
}
