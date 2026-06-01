package permcheck

import (
	"github.com/KomeiDiSanXian/remilia/builtin/core/permission"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
)

func HasPermission(permSvc *permission.Plugin, ctx *eventctx.Context, perm string) bool {
	if permSvc == nil {
		return true
	}
	return permSvc.HasPermission(ctx.GetUserID(), perm)
}
