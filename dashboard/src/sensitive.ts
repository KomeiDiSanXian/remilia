// 与服务端 api/config.go 的 sensitiveKeyNames 保持一致：
// 键名按“小写 + 去掉下划线”归一化后再精确匹配。
// config 包只带 yaml 标签，GET /config 返回 Go 字段名（Token、AccessToken），
// 只匹配全小写键名会漏掉这些敏感字段。
const SENSITIVE_KEYS = new Set(['api_key', 'token', 'secret', 'password', 'access_token'])

function normalizeKey(key: string): string {
  return key.toLowerCase().replace(/_/g, '')
}

export function isSensitiveKey(key: string): boolean {
  return SENSITIVE_KEYS.has(normalizeKey(key))
}
