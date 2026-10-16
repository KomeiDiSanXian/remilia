# 生成 Remilia 图标（一次性脚本；产物已提交）。
#
# 设计：靛蓝 → 紫罗兰对角渐变底 + 白色 R（带柔和投影），
# 与仪表盘主题色（--accent #6366f1）及仓库 favicon 的紫色调一致；
# 图形全部矢量重绘，任意尺寸都保持锐利。
#
# 用法（Windows）：
#   pwsh -File dashboard/scripts/generate-icons.ps1
#   cd desktop; npx tauri icon src-tauri/app-icon.json   # 由源图生成各平台图标
#
# 产物：
#   dashboard/public/icons/icon-192.png                 PWA 常规图标（192）
#   dashboard/public/icons/icon-512.png                 PWA 常规图标（512）
#   dashboard/public/icons/icon-maskable-512.png        Android 自适应图标（内容缩入 80% 安全区）
#   dashboard/public/icons/apple-touch-icon-180.png     iOS 添加到主屏幕图标（180）
#   desktop/src-tauri/app-icon.png                      桌面/移动端图标源图（1024）
#   desktop/src-tauri/app-icon-bg.png                   自适应图标背景层（渐变底，无字形）
#   desktop/src-tauri/app-icon-fg.png                   自适应图标前景层（透明底，只有字形）
#   desktop/src-tauri/app-icon-monochrome.png           主题图标单色层（Android 13+ 取色）

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
        [Parameter(Mandatory)][string]$OutPath,
        [double]$GlyphHeight = $RHeight,              # R 墨迹高度占画布比例
        [bool]$Background = $true,                    # 是否绘制渐变底（分层源图需要关掉）
        [bool]$Glyph = $true,                         # 是否绘制 R（纯背景层需要关掉）
        [bool]$GlyphShadow = $true                    # 是否给 R 加投影（单色层需要关掉）
    )

    # 4 倍超采样后缩回，边缘更平滑
    $render = $Size * 4
    $bmp = [System.Drawing.Bitmap]::new($render, $render)
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
    $g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
    $g.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality

    if ($Background) {
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
    }

    if ($Glyph) {
        # R：按墨迹包围盒等比缩放到目标高度并居中（等比缩放，字形不被拉伸）
        $family = [System.Drawing.FontFamily]::new('Segoe UI Black')
        $base = [System.Drawing.Drawing2D.GraphicsPath]::new()
        $base.AddString('R', $family, [int][System.Drawing.FontStyle]::Regular, 100,
            [System.Drawing.PointF]::new(0, 0), [System.Drawing.StringFormat]::GenericTypographic)
        $bounds = $base.GetBounds()

        $scale = ($render * $GlyphHeight * $ContentScale) / $bounds.Height
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

        if ($GlyphShadow) {
            # 柔和投影，让白字在浅色区域也有轮廓
            $shadowBrush = [System.Drawing.SolidBrush]::new($Shadow)
            $shadow = New-Transformed -Dx ($render * 0.014) -Dy ($render * 0.02)
            $g.FillPath($shadowBrush, $shadow)
            $shadow.Dispose()
            $shadowBrush.Dispose()
        }

        $inkBrush = [System.Drawing.SolidBrush]::new($Ink)
        $ink = New-Transformed -Dx 0 -Dy 0
        $g.FillPath($inkBrush, $ink)
        $ink.Dispose()
        $inkBrush.Dispose()

        $base.Dispose()
        $family.Dispose()
    }

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

# --- 桌面端 / 移动端图标源图 ---
# 分层原因：Android 自适应图标由系统把「背景层 + 前景层」合成后再套遮罩：
# 前景层要留出安全区（内容集中在画布中央约 2/3 以内），否则图形会被遮罩裁掉；
# 因此前景层里的 R 仍按 0.47 绘制，缩放交给 Tauri CLI（app-icon.json 的 android_fg_scale）。
$AppIconDir = Join-Path $PSScriptRoot '..\..\desktop\src-tauri'
New-RemiliaIcon -Size 1024 -ContentScale 1.0 -OutPath (Join-Path $AppIconDir 'app-icon.png')
New-RemiliaIcon -Size 512 -ContentScale 1.0 -Glyph $false -OutPath (Join-Path $AppIconDir 'app-icon-bg.png')
New-RemiliaIcon -Size 512 -ContentScale 1.0 -Background $false -OutPath (Join-Path $AppIconDir 'app-icon-fg.png')
New-RemiliaIcon -Size 512 -ContentScale 1.0 -Background $false -GlyphShadow $false -OutPath (Join-Path $AppIconDir 'app-icon-monochrome.png')
