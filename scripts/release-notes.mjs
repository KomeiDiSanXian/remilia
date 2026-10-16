// 从 CHANGELOG.md 抽取指定版本的段落，作为 GitHub Release 正文。
//
// 发布工作流在打标签时调用它，把结果交给 GoReleaser 的 --release-notes：
// 自动 changelog 只是一串被过滤过的提交与哈希，CHANGELOG 里的整理稿才是
// 给用户看的发布说明。找不到对应段落时以非 0 退出，工作流据此回退到
// GoReleaser 自带的 changelog，发布不会因此失败。
//
// 用法：node scripts/release-notes.mjs v1.2.3   （或设置 GITHUB_REF_NAME）
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

// 版本段落末尾附上的 compare 链接要用到，与 .goreleaser.yaml 里的仓库一致
const REPO_URL = 'https://github.com/KomeiDiSanXian/remilia'

const tag = (process.argv[2] ?? process.env.GITHUB_REF_NAME ?? '').trim()
if (!tag) {
  console.error('usage: node scripts/release-notes.mjs <tag>   (例如 v1.2.3)')
  process.exit(2)
}

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const lines = readFileSync(resolve(root, 'CHANGELOG.md'), 'utf8').split(/\r?\n/)

// 收集所有二级标题（"## Unreleased"、"## v1.73.0 (2026-10-16)"）
const headings = []
lines.forEach((line, index) => {
  const matched = /^##\s+(.+?)\s*$/.exec(line)
  if (matched) headings.push({ title: matched[1], index })
})

// 标题允许带日期后缀，但不能是别的版本的前缀（v1.73.0 不应命中 v1.73.0-rc1）
const found = headings.findIndex(({ title }) => title === tag || title.startsWith(`${tag} `))
if (found === -1) {
  console.error(`CHANGELOG.md 中没有 ${tag} 的段落（标题应形如 "## ${tag} (2026-01-01)"）`)
  process.exit(3)
}

const start = headings[found].index
const end = found + 1 < headings.length ? headings[found + 1].index : lines.length
const body = lines.slice(start + 1, end).join('\n').trim()
if (!body) {
  console.error(`${tag} 的段落是空的`)
  process.exit(4)
}

// 版本段落后面的第一个版本标题就是上一次发布，用来链接两个版本之间的全部提交
const previous = headings
  .slice(found + 1)
  .map(({ title }) => title.split(/\s+/)[0])
  .find((title) => /^v\d/.test(title))

const notes = [body]
if (previous && /^v\d/.test(tag)) {
  notes.push(`**Full Changelog**: ${REPO_URL}/compare/${previous}...${tag}`)
}
console.log(notes.join('\n\n'))
