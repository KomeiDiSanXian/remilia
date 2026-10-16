import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App.tsx'
import './style.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)

// PWA：注册 Service Worker，让仪表盘可"添加到主屏幕"并离线打开界面外壳。
// 仅在非 Tauri 的生产构建、安全上下文（https / localhost）中注册：
// 桌面端资源随安装包分发，注册 SW 只会引入缓存不确定性；
// 局域网 http 访问时浏览器本身也不允许注册。
if (
  import.meta.env.PROD &&
  !('__TAURI_INTERNALS__' in window) &&
  'serviceWorker' in navigator &&
  window.isSecureContext
) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js').catch((err) => {
      console.warn('[remilia] Service Worker 注册失败，仪表盘功能不受影响:', err)
    })
  })
}
