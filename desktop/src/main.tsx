import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App.tsx'
import { ErrorBoundary } from './components/ErrorBoundary.tsx'
import '@dashboard/style.css'
import './style.css'

// 全局捕获未处理的错误并显示在页面上
window.addEventListener('error', (e) => {
  const div = document.createElement('pre')
  div.style.cssText = 'color:#ef4444;padding:2rem;font-size:0.85rem;white-space:pre-wrap'
  div.textContent = `[Runtime Error]\n${e.error?.stack || e.message || e}`
  document.body.prepend(div)
})

window.addEventListener('unhandledrejection', (e) => {
  const div = document.createElement('pre')
  div.style.cssText = 'color:#ef4444;padding:2rem;font-size:0.85rem;white-space:pre-wrap'
  div.textContent = `[Unhandled Promise]\n${e.reason?.stack || e.reason}`
  document.body.prepend(div)
})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ErrorBoundary>
      <App />
    </ErrorBoundary>
  </StrictMode>,
)
