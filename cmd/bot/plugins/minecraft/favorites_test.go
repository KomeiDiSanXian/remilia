package minecraft

import (
	"context"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/storage"
)

func TestIsPrivateIP(t *testing.T) {
	private := []string{
		"127.0.0.1", "10.0.0.5", "172.16.0.1", "172.31.255.255",
		"192.168.1.100", "169.254.1.1", "100.64.0.1", "100.127.255.255",
		"::1", "fc00::1", "fd12:3456::1",
	}
	for _, s := range private {
		if !isPrivateIP(net.ParseIP(s)) {
			t.Errorf("%s 应判定为私有", s)
		}
	}
	public := []string{
		"8.8.8.8", "1.1.1.1", "172.32.0.1", "172.15.255.255",
		"100.128.0.1", "99.64.0.1", "2606:4700::1",
	}
	for _, s := range public {
		if isPrivateIP(net.ParseIP(s)) {
			t.Errorf("%s 不应判定为私有", s)
		}
	}
}

func TestIsPrivateTargetHostname(t *testing.T) {
	// 主机名：解析出私有地址时判定为私有
	if !isPrivateTarget("localhost") {
		t.Error("localhost 应判定为私有")
	}
	// 公网域名（依赖网络；解析失败时按非私有处理，测试在无网环境也不应失败）
	isPrivateTarget("mc.hypixel.net")
}

func TestParsePluginList(t *testing.T) {
	software, names := parsePluginList("Paper 1.21.1: WorldEdit, Essentials, LuckPerms")
	if software != "Paper 1.21.1" {
		t.Errorf("software = %q", software)
	}
	if len(names) != 3 || names[0] != "WorldEdit" {
		t.Errorf("names = %v", names)
	}

	// 无插件列表（vanilla 风格）
	software, names = parsePluginList("Vanilla 1.21.1")
	if software != "Vanilla 1.21.1" || len(names) != 0 {
		t.Errorf("software=%q names=%v", software, names)
	}
}

func TestProtocolVersionName(t *testing.T) {
	if got := protocolVersionName(767); got != "1.21" {
		t.Errorf("protocolVersionName(767) = %q", got)
	}
	if got := protocolVersionName(999999); got != "未知" {
		t.Errorf("未知协议应返回 未知, got %q", got)
	}
}

func TestDisplayVersionAndSoftware(t *testing.T) {
	status := &MCServerStatus{Version: "1.21.1", Protocol: 767, Software: "Paper 1.21.1", PluginCount: 3}
	if got := displayVersion(status); got != "1.21.1" {
		t.Errorf("displayVersion = %q", got)
	}
	if got := displaySoftware(status); got != "Paper 1.21.1（3 个插件）" {
		t.Errorf("displaySoftware = %q", got)
	}

	// 版本缺失时按协议号兜底
	status.Version = ""
	if got := displayVersion(status); got != "≈1.21（协议 767）" {
		t.Errorf("displayVersion fallback = %q", got)
	}
}

// TestFavManagerNilReceiverSafe 验证存储不可用（FavManager 为 nil）时
// 收藏相关方法返回错误而不是 panic。
// 依赖此契约：/mc list 在未启用 storage 插件时会回复"存储服务不可用"，
// 而不是把 "list" 当成主机名去查询。
func TestFavManagerNilReceiverSafe(t *testing.T) {
	var f *FavManager
	if f.Available() {
		t.Error("nil FavManager 不应可用")
	}
	if _, err := f.List("qq:1"); err == nil {
		t.Error("nil FavManager 的 List 应返回错误")
	}
	if _, err := f.Add("qq:1", "n", "a.example.com"); err == nil {
		t.Error("nil FavManager 的 Add 应返回错误")
	}
	if _, err := f.Remove("qq:1", "n"); err == nil {
		t.Error("nil FavManager 的 Remove 应返回错误")
	}
	if _, err := f.Get("qq:1", "n"); err == nil {
		t.Error("nil FavManager 的 Get 应返回错误")
	}
}

// newTestFavManager 用临时 SQLite 打开收藏管理器。
func newTestFavManager(t *testing.T) *FavManager {
	t.Helper()
	db, err := storage.Open(storage.WithDSN(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() {
		// 释放连接池，否则 Windows 上 t.TempDir 清理会因文件占用失败
		if sqlDB, err := db.DB().DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&FavServer{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return NewFavManager(db)
}

func TestFavManagerCRUD(t *testing.T) {
	fav := newTestFavManager(t)
	scope := "qq:12345"

	// 初始为空
	favs, err := fav.List(scope)
	if err != nil || len(favs) != 0 {
		t.Fatalf("初始列表 = %v, %v", favs, err)
	}

	// 添加
	if created, err := fav.Add(scope, "hypixel", "mc.hypixel.net"); err != nil || !created {
		t.Fatalf("Add = %v, %v", created, err)
	}
	if created, err := fav.Add(scope, "home", "192.168.1.100:25565"); err != nil || !created {
		t.Fatalf("Add = %v, %v", created, err)
	}

	// 同名覆盖
	created, err := fav.Add(scope, "hypixel", "mc2.hypixel.net")
	if err != nil || created {
		t.Errorf("同名应为更新: created=%v err=%v", created, err)
	}
	fav2, err := fav.Get(scope, "hypixel")
	if err != nil || fav2.Address != "mc2.hypixel.net" {
		t.Errorf("Get = %+v, %v", fav2, err)
	}

	// 列表顺序
	favs, _ = fav.List(scope)
	if len(favs) != 2 || favs[0].Name != "hypixel" || favs[1].Name != "home" {
		t.Errorf("List = %+v", favs)
	}

	// 序号与名称解析（复用 resolveTarget 逻辑的数据源）
	p := &mcPlugin{fav: fav}
	addr, err := p.resolveTarget(scope, "hypixel")
	if err != nil || addr != "mc2.hypixel.net" {
		t.Errorf("resolveTarget(name) = %q, %v", addr, err)
	}
	addr, err = p.resolveTarget(scope, "2")
	if err != nil || addr != "192.168.1.100:25565" {
		t.Errorf("resolveTarget(index) = %q, %v", addr, err)
	}
	addr, err = p.resolveTarget(scope, "mc.example.com:25565")
	if err != nil || addr != "mc.example.com:25565" {
		t.Errorf("resolveTarget(addr) = %q, %v", addr, err)
	}

	// 删除
	removed, err := fav.Remove(scope, "hypixel")
	if err != nil || !removed {
		t.Fatalf("Remove = %v, %v", removed, err)
	}
	removed, _ = fav.Remove(scope, "hypixel")
	if removed {
		t.Error("重复删除应返回 false")
	}
}

func TestFavManagerScopeIsolation(t *testing.T) {
	fav := newTestFavManager(t)
	_, err := fav.Add("group:A", "srv", "a.example.com")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	// 其他会话不可见
	if _, err := fav.Get("group:B", "srv"); err == nil {
		t.Error("不同会话不应共享收藏")
	}
}

func TestFavManagerCap(t *testing.T) {
	fav := newTestFavManager(t)
	scope := "group:cap"
	for i := range favMaxPerScope {
		if _, err := fav.Add(scope, string(rune('a'+i)), "a.example.com"); err != nil {
			t.Fatalf("Add #%d: %v", i, err)
		}
	}
	if _, err := fav.Add(scope, "over", "a.example.com"); err == nil {
		t.Error("超出上限应报错")
	}
}

// TestQueryNegativeCache 验证查询失败的负缓存：命中期间不重走网络。
func TestQueryNegativeCache(t *testing.T) {
	// 找一个无人监听的本地端口（连接拒绝，测试离线路径且不触外网）
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	p := &mcPlugin{
		cfg:      Config{Timeout: 2 * time.Second, CacheTTL: time.Minute, Avatars: false, EnableQuery: false, DirectQuery: true},
		client:   &http.Client{Timeout: 2 * time.Second},
		cache:    newTTLCache[*MCServerStatus](time.Minute, 8),
		errCache: newTTLCache[error](40*time.Millisecond, 8),
	}
	if _, queryErr := p.query(context.Background(), "127.0.0.1", port, "java"); queryErr == nil {
		t.Fatal("离线端口应报错")
	}

	key := "127.0.0.1|" + strconv.Itoa(port) + "|java"
	sentinel := errors.New("负缓存命中")
	p.errCache.set(key, sentinel)
	if _, err := p.query(context.Background(), "127.0.0.1", port, "java"); !errors.Is(err, sentinel) {
		t.Errorf("第二次查询应命中负缓存, got %v", err)
	}

	// 过期后应重新真实查询（返回真实错误而非哨兵）
	time.Sleep(60 * time.Millisecond)
	if _, err := p.query(context.Background(), "127.0.0.1", port, "java"); errors.Is(err, sentinel) {
		t.Error("负缓存过期后不应再返回缓存错误")
	}
}
