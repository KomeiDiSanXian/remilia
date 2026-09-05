package minecraft

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	// mcHeadsURLFormat mc-heads.net 头像 URL（支持 UUID 或玩家名）。
	mcHeadsURLFormat = "https://mc-heads.net/avatar/%s/32"
	// avatarMaxBytes 单个头像响应大小上限。
	avatarMaxBytes = 64 << 10
	// avatarFetchTimeout 拉取一批头像的总超时。
	avatarFetchTimeout = 5 * time.Second
	// avatarFetchConcurrency 单批拉取并发数上限。
	avatarFetchConcurrency = 8
	// avatarCacheTTL 头像缓存时长（玩家皮肤不常更换）。
	avatarCacheTTL = 10 * time.Minute
)

// avatarCache 玩家头像字节缓存（按玩家名/UUID）。
var avatarCache = newTTLCache[[]byte](avatarCacheTTL, 512)

// fetchPlayerHeads 并发拉取玩家头像（mc-heads.net），成功后写入 PlayerInfo.Head。
// 单个头像失败不影响其他玩家；整批受 avatarFetchTimeout 限制。
func fetchPlayerHeads(ctx context.Context, client *http.Client, players []PlayerInfo) {
	if len(players) == 0 || client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, avatarFetchTimeout)
	defer cancel()

	sem := make(chan struct{}, avatarFetchConcurrency)
	var wg sync.WaitGroup
	for i := range players {
		p := &players[i]
		id := headIdentifier(p)
		if id == "" || len(p.Head) > 0 {
			continue
		}
		if data, ok := avatarCache.get(id); ok {
			p.Head = data
			continue
		}
		wg.Go(func() {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			data, err := fetchPlayerHead(ctx, client, id)
			if err != nil || len(data) == 0 {
				return
			}
			avatarCache.set(id, data)
			p.Head = data
		})
	}
	wg.Wait()
}

// headIdentifier 返回头像请求标识。
// 正版（在线模式）服务器派发随机 v4 UUID，优先用 UUID 查询（玩家改名不影响）；
// 离线模式服务器的 UUID 是按玩家名派生的 v3 UUID，用它查 mc-heads 只会 404，
// 此时回退玩家名（mc-heads 按名解析，可命中同名的正版皮肤）。
func headIdentifier(p *PlayerInfo) string {
	if isPremiumUUID(p.UUID) {
		return p.UUID
	}
	for _, r := range p.Name {
		if ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9') || r == '_' {
			continue
		}
		return ""
	}
	return p.Name
}

// isPremiumUUID 判断是否为随机 v4 UUID（正版账号标识）。
// 离线模式服务器经 nameUUIDFromBytes 派生的是 v3 UUID（版本号位于第三组首字符）。
func isPremiumUUID(id string) bool {
	switch {
	case len(id) == 36 && id[8] == '-' && id[13] == '-' && id[18] == '-' && id[23] == '-':
		return id[14] == '4'
	case len(id) == 32: // 无连字符形式
		return id[12] == '4'
	}
	return false
}

func fetchPlayerHead(ctx context.Context, client *http.Client, id string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(mcHeadsURLFormat, id), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "RemiliaBot/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mc-heads HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, avatarMaxBytes))
}
