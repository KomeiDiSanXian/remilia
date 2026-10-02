# 曲库数据来源与许可

`songdle` 插件随包内置以下 maimai（舞萌DX）曲目数据，用于开箱即用的猜曲目游戏：

- `maimai.json` —— 曲目基础信息（曲名 / 曲师 / 流派 / 类型 / 版本 / BPM /
  EXPERT 与 MASTER 定数 / 谱师 / 绝赞数 / Re:MASTER 谱师）。
- `maimai_alias.json` —— 曲目别名（中文昵称、缩写等），用于按俗称搜索。

当前数据集由同目录下的生成器在 2026-10-02 生成：

```bash
cd cmd/bot
go run ./plugins/songdle/songs/gen          # 直接拉取上游接口
go run ./plugins/songdle/songs/gen -songs-src music_data.json -alias-src aliases.json  # 离线复现
```

上游来源都是无需鉴权、持续维护的公开接口：

- 曲目信息：[水鱼查分器](https://www.diving-fish.com/maimaidxprober/) 的
  `https://www.diving-fish.com/api/maimaidxprober/music_data`。
- 曲目别名：[Yuri-YuzuChaN/maimaiDX](https://github.com/Yuri-YuzuChaN/maimaiDX) 的别名服务
  `https://www.yuzuchan.moe/api/v2/aliases/maimaidx/aliases`。

生成时会做两处处理，与历代数据集保持一致：

- 只保留普通曲目（曲目 ID ≤ 100000），排除「宴会場」谱面 —— 它们曲名带 `[xxx]` 前缀、
  定数含 `?`，不适合作为谜底；
- 把水鱼数据里的日文 DX 版本名（如 `maimai でらっくす BUDDiES`）映射为国服年度版本名
  （`舞萌DX2024`）。

数据格式沿用早期取自 [Maidle](https://github.com/Dale2003/maidle)（MIT License）的数据集，
字段保持不变以便兼容；曲目名称、BPM、定数等属于游戏事实性数据，在此仅用于本地猜谜游戏，
版权归原权利方（SEGA / 各曲师）所有，别名数据由玩家社区整理、同样仅作游戏用途。

如需替换为自己的曲库，把 `plugins.songdle.songs_file` 指向自备数据集，或设置
`songs_url` / `alias_url` 从远端拉取即可；两者优先级都高于内置数据。若完全不需要
内置 maimai 数据，可在构建时删除本目录下的两个 JSON（插件会要求配置曲库）。
