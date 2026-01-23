package token

import (
	"strconv"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"

	"github.com/KomeiDiSanXian/remilia/httpcilent"
	"github.com/KomeiDiSanXian/remilia/openapi/constant"
	"github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

// Manager 管理 access token
//
// https://bot.q.qq.com/wiki/develop/api-v2/dev-prepare/interface-framework/api-use.html#%E8%8E%B7%E5%8F%96%E8%B0%83%E7%94%A8%E5%87%AD%E8%AF%81
type Manager struct {
	mu          sync.Mutex
	cond        *sync.Cond
	accessToken string
	expiresAt   time.Time
	ready       bool
}

// NewManager 初始化并启动后台刷新
func NewManager(info *dto.BotInfo) *Manager {
	m := &Manager{}
	m.cond = sync.NewCond(&m.mu)
	go m.autoRefresh(info)
	return m
}

// WaitReady 阻塞直到 access token 可用
func (m *Manager) WaitReady() {
	m.mu.Lock()
	for !m.ready {
		m.cond.Wait() // 等到ready为true时被唤醒
	}
	m.mu.Unlock()
}

// GetToken 获取当前的 access token
func (m *Manager) GetToken() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.accessToken
}

func (m *Manager) autoRefresh(info *dto.BotInfo) {
	var firstSuccess bool
	for {
		resp, err := requestToken(info)
		if err != nil {
			logrus.WithError(err).Warn("[Token] Failed to get access token")
			time.Sleep(10 * time.Second) // 失败后等待一段时间再重试
			continue
		}
		m.mu.Lock()
		m.accessToken = resp.Get("access_token").Str
		expiresIn := resp.Get("expires_in").Int()
		m.expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
		// 首次成功获取到 token 后，设置 ready 并广播
		if !firstSuccess {
			m.ready = true
			firstSuccess = true
			m.cond.Broadcast()
		} else {
			// TODO: 可以在这里添加一个回调函数，通知 token 更新了
		}
		m.mu.Unlock()
		logrus.WithField("access_token", m.accessToken).WithField("expires_in", expiresIn).Debug("[Token] Updated")

		refreshAfter := time.Duration(expiresIn-30) * time.Second // 提前30秒刷新
		if refreshAfter <= 0 {
			refreshAfter = time.Duration(expiresIn) * time.Second / 2 // 如果expires_in小于60秒，则在一半时间后刷新
		}
		time.Sleep(refreshAfter)
	}

}

func requestToken(info *dto.BotInfo) (gjson.Result, error) {
	bodyMap := map[string]string{
		"appId":        strconv.FormatUint(info.AppID, 10),
		"clientSecret": info.AppSecret,
	}
	return httpcilent.NewPost(constant.AccessTokenURL).SetJSONBody(bodyMap).DoJSON()
}
