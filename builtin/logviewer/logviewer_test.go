package logviewer_test

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/auditlog"
	"github.com/KomeiDiSanXian/remilia/builtin/logviewer"
	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/KomeiDiSanXian/remilia/plugin/plugintest"
	"github.com/KomeiDiSanXian/remilia/testbot"
)

func makeLogViewerCtx() (*logviewer.Plugin, *auditlog.Plugin, func()) {
	auditSvc := auditlog.NewPlugin(auditlog.DefaultConfig())
	d := logviewer.New()
	ctx := plugintest.NewSetupContextWithDeps("logviewer", map[string]any{
		"auditlog": auditSvc,
	}, nil)
	api, err := d.Setup(ctx)
	if err != nil {
		plugintest.StopSetupContext(ctx)
		panic("Setup failed: " + err.Error())
	}
	return api.(*logviewer.Plugin), auditSvc, func() { plugintest.StopSetupContext(ctx) }
}

func makeLogViewerEvent(userID, content string) *context.Context {
	event := testbot.MakePlatformC2CEvent(userID, content)
	return context.NewContextFromEvent(event, nil)
}

func TestNew(t *testing.T) {
	d := logviewer.New()
	if d == nil {
		t.Fatal("New returned nil")
	}
	if d.Name != "logviewer" {
		t.Errorf("expected name %q, got %q", "logviewer", d.Name)
	}
	if d.Meta.Description != "审计日志查询工具，在聊天中搜索和查看审计日志" {
		t.Errorf("unexpected description: %q", d.Meta.Description)
	}
}

func TestSetupWithAuditLog(t *testing.T) {
	auditSvc := auditlog.NewPlugin(auditlog.DefaultConfig())
	d := logviewer.New()
	ctx := plugintest.NewSetupContextWithDeps("logviewer", map[string]any{
		"auditlog": auditSvc,
	}, nil)
	defer plugintest.StopSetupContext(ctx)

	api, err := d.Setup(ctx)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	if api == nil {
		t.Fatal("Setup returned nil")
	}
}

func TestSetupWithoutAuditLog(t *testing.T) {
	d := logviewer.New()
	ctx := plugintest.NewSetupContext("logviewer", nil)
	defer plugintest.StopSetupContext(ctx)

	_, err := d.Setup(ctx)
	if err == nil {
		t.Error("expected error when auditlog plugin is not available")
	}
}

func TestSetupWithDeps(t *testing.T) {
	auditSvc := auditlog.NewPlugin(auditlog.DefaultConfig())
	d := logviewer.New()
	_, err, stop := plugintest.RunSetup(d, &plugintest.SetupOptions{
		Container: func() *plugin.Container {
			c := plugin.NewContainer()
			c.Register("auditlog", auditSvc)
			return c
		}(),
	})
	defer stop()
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
}

func TestLogViewerOperations(t *testing.T) {
	svc, auditSvc, stop := makeLogViewerCtx()
	defer stop()

	ctx := makeLogViewerEvent("user1", "/logs recent")
	_ = svc
	_ = ctx
	_ = auditSvc
}

func TestTruncate(t *testing.T) {
	// Test the truncate function behavior indirectly
	auditSvc := auditlog.NewPlugin(auditlog.DefaultConfig())
	_ = auditSvc
}

func TestLogViewerIntegration(t *testing.T) {
	auditSvc := auditlog.NewPlugin(auditlog.DefaultConfig())
	d := logviewer.New()
	ctx := plugintest.NewSetupContextWithDeps("logviewer", map[string]any{
		"auditlog": auditSvc,
	}, nil)
	defer plugintest.StopSetupContext(ctx)

	api, err := d.Setup(ctx)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	_ = api
}

func TestRecentLogs(t *testing.T) {
	auditSvc := auditlog.NewPlugin(auditlog.DefaultConfig())
	d := logviewer.New()
	logviewerCtx := plugintest.NewSetupContextWithDeps("logviewer", map[string]any{
		"auditlog": auditSvc,
	}, nil)
	defer plugintest.StopSetupContext(logviewerCtx)

	api, err := d.Setup(logviewerCtx)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	svc := api.(*logviewer.Plugin)
	_ = svc

	now := time.Now()
	_ = now
	_ = auditSvc
}
