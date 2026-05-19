# 构建 WASM 测试插件
# 需要 Go 1.21+（支持 wasip1/wasm 平台）
$env:GOOS="wasip1"
$env:GOARCH="wasm"
go build -o testplugin.wasm .
Write-Output "Built testplugin.wasm ($((Get-Item testplugin.wasm).Length) bytes)"
