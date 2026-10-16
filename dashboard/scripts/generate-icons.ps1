# 生成仪表盘 PWA 图标（一次性脚本；产物已提交到 dashboard/public/icons/）。
#
# 设计：靛蓝 → 紫罗兰对角渐变底 + 白色 R（带柔和投影），
# 与仪表盘主题色（--accent #6366f1）及仓库 favicon 的紫色调一致；
# 图形全部矢量重绘，任意尺寸都保持锐利。
#
# 用法（Windows）：
#   pwsh -File dashboard/scripts/generate-icons.ps1
#
# 产物：
#   icon-192.png                 PWA 常规图标（192）
#   icon-512.png                 PWA 常规图标（512）
#   icon-maskable-512.png        Android 自适应图标（内容缩入 80% 安全区）
#   apple-touch-icon-180.png     iOS 添加到主屏幕图标（180）

Add-Type -AssemblyName System.Drawing

$OutDir = Join-Path $PSScriptRoot '..\public\icons'
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

# 配色
$BgFrom = '#4F46E5'   # 靛蓝
$BgTo = '#9333EA'     # 紫罗兰
$Ink = [System.Drawing.Color]::White
$Shadow = [System.Drawing.Color]::FromArgb(72, 26, 16, 72)

# R 墨迹高度占画布比例（maskable 时再乘 ContentScale）
$RHeight = 0.47

function New-RemiliaIcon {
    param(
        [Parameter(Mandatory)][int]$Size,
        [Parameter(Mandatory)][double]$ContentScale,  # 1.0 = 常规；<1 表示图形缩小（maskable 安全区）
        [Parameter(Mandatory)][string]$OutPath
    )

    # 4 倍超采样后缩回，边缘更平滑
    $render = $Size * 4
    $bmp = [System.Drawing.Bitmap]::new($render, $render)
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
    $g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
    $g.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality

    # 背景：对角渐变铺满整块画布（launcher 的圆形/圆角遮罩由系统施加，图标本身不做裁切）
    $bgBrush = [System.Drawing.Drawing2D.LinearGradientBrush]::new(
        [System.Drawing.PointF]::new(0, 0),
        [System.Drawing.PointF]::new($render, $render),
        [System.Drawing.ColorTranslator]::FromHtml($BgFrom),
        [System.Drawing.ColorTranslator]::FromHtml($BgTo))
    $g.FillRectangle($bgBrush, 0, 0, $render, $render)
    $bgBrush.Dispose()

    # 顶部一层柔和高光，模拟自上而下的光照，避免大面积纯色发闷
    $gloss = [System.Drawing.Drawing2D.LinearGradientBrush]::new(
        [System.Drawing.PointF]::new(0, 0),
        [System.Drawing.PointF]::new(0, [float]($render * 0.62)),
        [System.Drawing.Color]::FromArgb(38, 255, 255, 255),
        [System.Drawing.Color]::FromArgb(0, 255, 255, 255))
    $g.FillRectangle($gloss, 0, 0, $render, [float]($render * 0.62))
    $gloss.Dispose()

    # R：按墨迹包围盒等比缩放到目标高度并居中（等比缩放，字形不被拉伸）
    $family = [System.Drawing.FontFamily]::new('Segoe UI Black')
    $base = [System.Drawing.Drawing2D.GraphicsPath]::new()
    $base.AddString('R', $family, [int][System.Drawing.FontStyle]::Regular, 100,
        [System.Drawing.PointF]::new(0, 0), [System.Drawing.StringFormat]::GenericTypographic)
    $bounds = $base.GetBounds()

    $scale = ($render * $RHeight * $ContentScale) / $bounds.Height
    $w = $bounds.Width * $scale
    $h = $bounds.Height * $scale
    $x = ($render - $w) / 2
    $y = ($render - $h) / 2

    # 注意 GDI+ 矩阵的 Prepend 语义：先指定的变换最后作用于点，
    # 因此按"目标平移 → 缩放 → 归一化到原点"的逆序书写。
    function New-Transformed {
        param([double]$Dx, [double]$Dy)
        $path = $base.Clone()
        $matrix = [System.Drawing.Drawing2D.Matrix]::new()
        $matrix.Translate(($x + $Dx), ($y + $Dy))
        $matrix.Scale($scale, $scale)
        $matrix.Translate(-$bounds.X, -$bounds.Y)
        $path.Transform($matrix)
        $matrix.Dispose()
        return $path
    }

    # 柔和投影，让白字在浅色区域也有轮廓
    $shadowBrush = [System.Drawing.SolidBrush]::new($Shadow)
    $shadow = New-Transformed -Dx ($render * 0.014) -Dy ($render * 0.02)
    $g.FillPath($shadowBrush, $shadow)
    $shadow.Dispose()
    $shadowBrush.Dispose()

    $inkBrush = [System.Drawing.SolidBrush]::new($Ink)
    $ink = New-Transformed -Dx 0 -Dy 0
    $g.FillPath($inkBrush, $ink)
    $ink.Dispose()
    $inkBrush.Dispose()

    $base.Dispose()
    $family.Dispose()

    # 缩回目标尺寸
    $final = [System.Drawing.Bitmap]::new($Size, $Size)
    $g2 = [System.Drawing.Graphics]::FromImage($final)
    $g2.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
    $g2.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
    $g2.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::HighQuality
    $g2.DrawImage($bmp, [System.Drawing.Rectangle]::new(0, 0, $Size, $Size))

    $final.Save($OutPath, [System.Drawing.Imaging.ImageFormat]::Png)
    $g2.Dispose()
    $final.Dispose()
    $g.Dispose()
    $bmp.Dispose()

    Write-Host ("generated {0} ({1}x{1}, content scale {2})" -f (Split-Path $OutPath -Leaf), $Size, $ContentScale)
}

# 常规图标
New-RemiliaIcon -Size 192 -ContentScale 1.0 -OutPath (Join-Path $OutDir 'icon-192.png')
New-RemiliaIcon -Size 512 -ContentScale 1.0 -OutPath (Join-Path $OutDir 'icon-512.png')

# 可遮罩图标：Android 自适应图标安全区为直径 80% 的圆，图形再收一档
New-RemiliaIcon -Size 512 -ContentScale 0.74 -OutPath (Join-Path $OutDir 'icon-maskable-512.png')

# iOS 主屏幕图标（由系统做圆角遮罩）
New-RemiliaIcon -Size 180 -ContentScale 1.0 -OutPath (Join-Path $OutDir 'apple-touch-icon-180.png')
