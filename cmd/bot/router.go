package main

import (
	"github.com/KomeiDiSanXian/remilia/core/fsm"
	"github.com/KomeiDiSanXian/remilia/router"
)

// setupRouter 创建命令路由并注入 Bot，FSM 管理器保存到 app 供 API 服务器使用。
func (a *app) setupRouter() {
	fsmMgr := fsm.NewManager(nil)
	rtr := router.New(a.eng, fsmMgr.Engine())
	rtr.Route(router.WithCommandPrefix())
	a.bot.UseRouter(rtr)
	a.fsmMgr = fsmMgr
}
