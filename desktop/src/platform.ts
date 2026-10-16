/**
 * 运行环境判定。
 *
 * 桌面端与移动端（Android / iOS）共用同一套前端：
 * 托盘、自动更新、内置 Go 后端 sidecar 等能力只有桌面端有，
 * 移动端需要隐藏对应入口，避免点了以后只报错。
 */
export const isMobileShell = /Android|iPhone|iPad|iPod/i.test(navigator.userAgent)
