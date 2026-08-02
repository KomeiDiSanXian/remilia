package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// apiBase GitHub REST API 根地址（var 以便测试注入 mock）。
var apiBase = "https://api.github.com"

// ErrNoRelease 仓库没有任何可用的 Release（也可能是 repo 配置错误/仓库不存在）。
var ErrNoRelease = errors.New("仓库没有可用的 Release（检查 plugins.updater.repo 配置）")

// ReleaseAsset 是 GitHub Release 的一个资产（归档文件）。
type ReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Release 是 GitHub Release 的元数据。
type Release struct {
	TagName    string         `json:"tag_name"`
	Draft      bool           `json:"draft"`
	Prerelease bool           `json:"prerelease"`
	Assets     []ReleaseAsset `json:"assets"`
}

// Asset 按名字查找发布资产，不存在时返回 false。
func (r *Release) Asset(name string) (ReleaseAsset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return ReleaseAsset{}, false
}

// githubClient 是 GitHub Releases API 的极简客户端（无外部依赖）。
type githubClient struct {
	owner, repo string
	hc          *http.Client
}

// newGitHubClient 创建 GitHub API 客户端。
// proxy 为空时使用环境变量代理或直连；timeout 为单请求超时。
func newGitHubClient(owner, repo, proxy string, timeout time.Duration) *githubClient {
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment}
	if proxy != "" {
		if u, err := url.Parse(proxy); err == nil {
			transport = &http.Transport{Proxy: http.ProxyURL(u)}
		}
	}
	return &githubClient{
		owner: owner,
		repo:  repo,
		hc:    &http.Client{Transport: transport, Timeout: timeout},
	}
}

// latestRelease 获取最新 Release。
// allowPrerelease 为 false 时使用 /releases/latest（恒为正式版）；
// 为 true 时列出最近 Release 取第一个非 Draft（可能包含预发布版）。
func (c *githubClient) latestRelease(ctx context.Context, allowPrerelease bool) (*Release, error) {
	if !allowPrerelease {
		var rel Release
		if err := c.fetchJSON(ctx, fmt.Sprintf("%s/repos/%s/%s/releases/latest", apiBase, c.owner, c.repo), &rel); err != nil {
			return nil, err
		}
		return &rel, nil
	}

	var rels []Release
	if err := c.fetchJSON(ctx, fmt.Sprintf("%s/repos/%s/%s/releases?per_page=100", apiBase, c.owner, c.repo), &rels); err != nil {
		return nil, err
	}
	for i := range rels {
		if !rels[i].Draft {
			return &rels[i], nil
		}
	}
	return nil, ErrNoRelease
}

// fetchJSON 发起 GET 请求并将 JSON 响应解码到 out。
// 404 映射为 ErrNoRelease，403 给出限流提示，其余非 2xx 给出状态码错误。
func (c *githubClient) fetchJSON(ctx context.Context, apiURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "remilia-updater")

	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("请求 GitHub API 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return ErrNoRelease
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("GitHub API 限流（%d）：匿名访问每小时 60 次，请稍后重试", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("GitHub API 返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return fmt.Errorf("解析 GitHub API 响应失败: %w", err)
	}
	return nil
}
