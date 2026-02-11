# Debug 子命令演示程序启动脚本

Write-Host "🚀 Debug 子命令演示程序" -ForegroundColor Cyan
Write-Host "================================" -ForegroundColor Cyan
Write-Host ""

# 检查环境变量
if (-not $env:BOT_APPID) {
    Write-Host "❌ 错误: 未设置 BOT_APPID 环境变量" -ForegroundColor Red
    Write-Host ""
    Write-Host "请先设置环境变量:" -ForegroundColor Yellow
    Write-Host '  $env:BOT_APPID="你的机器人AppID"' -ForegroundColor Yellow
    Write-Host '  $env:BOT_TOKEN="你的机器人Token"' -ForegroundColor Yellow
    Write-Host '  $env:ADMIN_USER_ID="管理员用户ID"  # 可选' -ForegroundColor Yellow
    exit 1
}

if (-not $env:BOT_TOKEN) {
    Write-Host "❌ 错误: 未设置 BOT_TOKEN 环境变量" -ForegroundColor Red
    Write-Host ""
    Write-Host "请先设置环境变量:" -ForegroundColor Yellow
    Write-Host '  $env:BOT_APPID="你的机器人AppID"' -ForegroundColor Yellow
    Write-Host '  $env:BOT_TOKEN="你的机器人Token"' -ForegroundColor Yellow
    Write-Host '  $env:ADMIN_USER_ID="管理员用户ID"  # 可选' -ForegroundColor Yellow
    exit 1
}

Write-Host "✅ 环境变量检查通过" -ForegroundColor Green
Write-Host "   BOT_APPID: $env:BOT_APPID" -ForegroundColor Gray

if ($env:ADMIN_USER_ID) {
    Write-Host "   ADMIN_USER_ID: $env:ADMIN_USER_ID" -ForegroundColor Gray
} else {
    Write-Host "   ADMIN_USER_ID: 未设置（开发模式）" -ForegroundColor Gray
}

Write-Host ""
Write-Host "📦 安装依赖..." -ForegroundColor Cyan
go mod tidy

Write-Host ""
Write-Host "▶️  启动程序..." -ForegroundColor Cyan
Write-Host ""

go run main.go

