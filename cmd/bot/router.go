package main

import (
	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/core/fsm"
	"github.com/KomeiDiSanXian/remilia/router"
)

// setupRouter 创建命令路由并注入 Bot，返回关联的 FSM 管理器。
func setupRouter(bot *remilia.Bot, eng *engine.Engine) *fsm.Manager {
	fsmMgr := fsm.NewManager(nil)
	rtr := router.New(eng, fsmMgr.Engine())
	rtr.Route(router.WithCommandPrefix())
	bot.UseRouter(rtr)
	return fsmMgr
}
