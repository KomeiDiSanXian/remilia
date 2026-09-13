package main

// commit / date 由构建期通过 ldflags 注入
// （Makefile 与 .goreleaser.yaml 均使用 -X main.commit / -X main.date）。
// 因此这两个变量必须留在 main 包，不能随装配逻辑迁出。
var (
	commit string
	date   string
)

// main 仅负责启动应用；装配、启动与关闭流程见 app.run（app.go）。
func main() {
	newApp().run()
}
