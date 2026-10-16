/* Remilia Dashboard Service Worker
 *
 * 目标：让仪表盘能在手机上"添加到主屏幕"，并在网络不稳时仍能打开界面外壳。
 *
 * 策略：
 *   - 只处理同源 GET 请求；/api/（含日志 SSE 流）一律直连网络，不缓存、不拦截。
 *   - 页面导航：网络优先，断网时回退缓存的 index.html。
 *   - /assets/ 下的构建产物带内容哈希，缓存优先（文件名变化天然免失效）。
 *   - 其余同源静态资源（favicon、图标等）：网络优先，失败回退缓存。
 *
 * 注意：Service Worker 只在安全上下文（https 或 localhost）注册；
 * 通过局域网 http://<host>:9002 访问时浏览器不会注册，仪表盘功能不受影响。
 */

const CACHE = 'remilia-dashboard-v1'
const SHELL = ['./', './index.html']
const SHELL_EXTRA = ['./manifest.json', './favicon.svg']

self.addEventListener('install', (event) => {
  event.waitUntil((async () => {
    const cache = await caches.open(CACHE)
    // 外壳必需文件失败则本次安装失败（下次访问重试）；附加文件失败不影响安装
    await cache.addAll(SHELL)
    await Promise.all(SHELL_EXTRA.map((url) => cache.add(url).catch(() => {})))
    // 构建产物文件名带内容哈希，无法写死：从 index.html 解析后一并预缓存，
    // 保证安装后第一次断网也能完整打开界面（失败不影响安装）
    try {
      const html = await (await cache.match('./index.html')).text()
      const assets = [...html.matchAll(/(?:src|href)="(\/assets\/[^"]+)"/g)].map((m) => m[1])
      await Promise.all(assets.map((url) => cache.add(url).catch(() => {})))
    } catch {
      /* 忽略：外壳不完整时仍可安装 */
    }
    // 新版本立即接管，避免用户停留在旧版本外壳
    await self.skipWaiting()
  })())
})

self.addEventListener('activate', (event) => {
  event.waitUntil((async () => {
    const keys = await caches.keys()
    await Promise.all(keys.filter((key) => key !== CACHE).map((key) => caches.delete(key)))
    await self.clients.claim()
  })())
})

self.addEventListener('fetch', (event) => {
  const req = event.request
  if (req.method !== 'GET') return

  const url = new URL(req.url)
  if (url.origin !== self.location.origin) return
  // 管理 API 与日志 SSE 流必须直连：缓存会破坏实时性，也会丢认证头
  if (url.pathname.startsWith('/api/')) return

  // 页面导航：网络优先，离线回退外壳
  if (req.mode === 'navigate') {
    event.respondWith((async () => {
      const cache = await caches.open(CACHE)
      try {
        const res = await fetch(req)
        if (res.ok) await cache.put('./index.html', res.clone())
        return res
      } catch (err) {
        const hit = await cache.match('./index.html')
        if (hit) return hit
        throw err
      }
    })())
    return
  }

  // 带内容哈希的构建产物：缓存优先
  if (url.pathname.startsWith('/assets/')) {
    event.respondWith((async () => {
      const cache = await caches.open(CACHE)
      const hit = await cache.match(req)
      if (hit) return hit
      const res = await fetch(req)
      if (res.ok) await cache.put(req, res.clone())
      return res
    })())
    return
  }

  // 其余静态资源：网络优先，失败回退缓存
  event.respondWith((async () => {
    const cache = await caches.open(CACHE)
    try {
      const res = await fetch(req)
      if (res.ok) await cache.put(req, res.clone())
      return res
    } catch (err) {
      const hit = await cache.match(req)
      if (hit) return hit
      throw err
    }
  })())
})
