# 构建 Showcase WASM 演示插件（需要 TinyGo 0.31+）
$env:GOWORK = "off"
tinygo build -o ..\demo.wasm -target=wasi .
Write-Output "Built demo.wasm ($((Get-Item ..\demo.wasm).Length) bytes)"
