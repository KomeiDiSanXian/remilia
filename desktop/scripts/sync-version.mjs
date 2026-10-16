// 把桌面端版本号同步为发布标签（去掉前缀 v）。
//
// 桌面端内置 Go 后端子进程，每个框架版本都会带出新的 sidecar，
// 因此每次 tag 发布都应视为一次桌面端发布。版本号不跟随标签的话，
// updater 里 latest.json 的版本永远等于本地版本，更新永远不会被提示。
//
// 用法：node scripts/sync-version.mjs v1.2.3
import { readFileSync, writeFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const tag = process.argv[2]
if (!tag) {
  console.error('usage: node scripts/sync-version.mjs <tag>   (例如 v1.2.3)')
  process.exit(1)
}

const version = tag.replace(/^v/, '')
if (!/^\d+\.\d+\.\d+/.test(version)) {
  console.error(`无法从标签解析版本号: ${tag}`)
  process.exit(1)
}

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')

const confPath = resolve(root, 'src-tauri/tauri.conf.json')
const conf = JSON.parse(readFileSync(confPath, 'utf8'))
conf.version = version
writeFileSync(confPath, `${JSON.stringify(conf, null, 2)}\n`)

const pkgPath = resolve(root, 'package.json')
const pkg = JSON.parse(readFileSync(pkgPath, 'utf8'))
pkg.version = version
writeFileSync(pkgPath, `${JSON.stringify(pkg, null, 2)}\n`)

const cargoPath = resolve(root, 'src-tauri/Cargo.toml')
const cargo = readFileSync(cargoPath, 'utf8')
const updated = cargo.replace(/^version = "[^"]*"/m, `version = "${version}"`)
if (updated === cargo) {
  console.error('未在 Cargo.toml 中找到版本号行')
  process.exit(1)
}
writeFileSync(cargoPath, updated)

console.log(`桌面端版本已更新为 ${version}`)
