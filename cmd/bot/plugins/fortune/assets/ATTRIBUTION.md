# 占卜素材来源与授权

本目录下的图片资源用于 `/omikuji` 与 `/tarot` 命令的牌面渲染。所有素材均已随源码
编译进二进制（见 `../assets.go` 的 `//go:embed`），运行时不访问任何外部图床。

## assets/omikuji/ — 浅草寺御神签签纸扫描

- 内容：100 番 × 2 页，共 200 张签纸扫描（`<nnn>_<v>.webp`）
  - `_0` 签文页：番号、吉凶、漢詩（五言四句）
  - `_1` 解签页：番号、吉凶、漢詩逐句解释，以及願望／病気／待人／失物／旅行等分类运势
  - 两页内容互补，命令会并排合成一张图发送
- 来源：<https://github.com/fumiama/senso-ji-omikuji>
- 授权：MIT License
- 处理：等比缩放至 460px 宽并转码为 WebP，仅作体积优化，未修改画面内容

依据 MIT 许可，分发时须保留原始版权声明与许可声明。原始许可声明（节录）：

> MIT License
>
> Copyright (c) fumiama
>
> Permission is hereby granted, free of charge, to any person obtaining a copy
> of this software and associated documentation files (the "Software"), to deal
> in the Software without restriction, including without limitation the rights
> to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
> copies of the Software, and to permit persons to whom the Software is
> furnished to do so, subject to the following conditions:
>
> The above copyright notice and this permission notice shall be included in all
> copies or substantial portions of the Software.
>
> THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
> IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
> FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.

## assets/tarot/ — 韦特塔罗牌面（Rider-Waite-Smith）

- 内容：大阿尔卡纳 22 张（`ar00`–`ar21`）+ 小阿尔卡纳 56 张（`wa`/`cu`/`sw`/`pe` 各 14 张），共 78 张
- 来源：Wikimedia Commons；如 `File:RWS Tarot 00 Fool.jpg`、`File:Wands01.jpg`
- 授权：公有领域（Public Domain）
- 处理：等比缩放至 340px 宽并转码为 WebP，仅作体积优化

授权依据：该牌组由 A. E. Waite 与 Pamela Colman Smith 创作，1909 年首次出版于伦敦；
作者已于 1951 年逝世。作品在美国因 1929 年前出版而进入公有领域，在作者逝世逾 70 年
的司法辖区亦已届满版权保护期。
