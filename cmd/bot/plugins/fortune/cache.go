package fortune

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// imageCache 管理外部图片的下载与磁盘缓存。
// 以 MD5 哈希作为缓存键，首次下载后永久缓存到本地。
type imageCache struct {
	dir string
}

// newImageCache 创建图片缓存目录，dir 为空时使用默认路径。
func newImageCache(dir string) *imageCache {
	if dir == "" {
		dir = filepath.Join(".", "data", "fortune", "images")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		panic(fmt.Errorf("fortune: create image cache dir: %w", err))
	}
	return &imageCache{dir: dir}
}

// cachePath 根据缓存键计算文件路径（MD5 哈希 + .jpg）。
func (c *imageCache) cachePath(key string) string {
	h := md5.Sum([]byte(key))
	name := hex.EncodeToString(h[:]) + ".jpg"
	return filepath.Join(c.dir, name)
}

// Get 返回缓存图片。缓存命中则从磁盘读取，否则从 url 下载并保存。
func (c *imageCache) Get(ctx context.Context, key, url string) (image.Image, error) {
	path := c.cachePath(key)
	if img, err := loadImage(path); err == nil {
		return img, nil
	}
	return c.downloadAndSave(ctx, url, path)
}

// loadImage 从磁盘文件解码图片。
func loadImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

// downloadAndSave 从 URL 下载图片并缓存到磁盘。
func (c *imageCache) downloadAndSave(ctx context.Context, url, path string) (image.Image, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "RemiliaBot/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status: %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return nil, err
	}
	return img, nil
}

// getCache 返回 Plugin 实例级别的缓存，每个 Plugin 有独立的缓存目录。
func getCache(p *Plugin) *imageCache { //nolint:unused
	if p.cache == nil {
		p.cache = newImageCache(p.dataDir)
	}
	return p.cache
}
