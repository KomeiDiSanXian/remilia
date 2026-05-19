# 构建 TinyGo WASM 测试插件
# 需要安装 TinyGo 0.31+ 和 binaryen（wasm-opt）
# 并且需要 Go 1.23.x（TinyGo 0.34 兼容版本）

$tinygoBin = "tinygo"  # 确保 tinygo 在 PATH 中
$goRoot = "go1.23.0"   # TinyGo 0.34 需要 Go 1.23

# 设置环境
$env:GOWORK = "off"

tinygo build -o tinygoplugin.wasm -target=wasi .
if ($LASTEXITCODE -eq 0) {
    Copy-Item tinygoplugin.wasm ..\ -Force
    Write-Output "Built tinygoplugin.wasm ($((Get-Item tinygoplugin.wasm).Length) bytes)"
} else {
    Write-Error "Build failed"
    exit 1
}
