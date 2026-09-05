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

// headIdentifier 返回头像请求标识：优先 UUID，否则校验玩家名合法性。
func headIdentifier(p *PlayerInfo) string {
	if p.UUID != "" {
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
