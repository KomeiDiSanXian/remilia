# 词库来源与授权

本目录下的 `allowed_*.txt`、`answers_*.txt`、`obscure_*.txt` 与 `excluded.txt`
由以下公开数据集生成：

- 英文单词表：[dwyl/english-words](https://github.com/dwyl/english-words)
  （`words_alpha.txt`，Unlicense / 公有领域）
- 词频表：[hermitdave/FrequencyWords](https://github.com/hermitdave/FrequencyWords)
  （`content/2018/en/en_50k.txt`，MIT License）
- 通用词频（Zipf）：[wordfreq](https://github.com/rspeer/wordfreq)（MIT License）
- 非专有名词词表：[dolph/dictionary](https://github.com/dolph/dictionary)
  （`enable1.txt`，ENABLE 词表）
- 人名：美国人口普查 1990 常用名（`dist.male.first` / `dist.female.first`）、
  [hadley/data-baby-names](https://github.com/hadley/data-baby-names)（SSA，公有领域）、
  [dominictarr/random-name](https://github.com/dominictarr/random-name)
- 地名：国家/首都/美国州名，来自
  [dr5hn/countries-states-cities-database](https://github.com/dr5hn/countries-states-cities-database)（ODbL）
- 粗俗/攻击性词表：
  [LDNOOBW](https://github.com/LDNOOBW/List-of-Dirty-Naughty-Obscene-and-Otherwise-Bad-Words)（CC-BY 4.0）

生成规则：

- `allowed_<L>.txt`：长度 L、仅含 `a-z`、且出现在 `en_50k` 词频表中的单词（去重、排序）。
- `answers_<L>.txt`：在 `allowed` 基础上要求同时满足
  （1）出现在 ENABLE 词表（排除绝大多数专有名词）；
  （2）`wordfreq` 英语 Zipf 词频 ≥ 3.6（各长度统一常见度下限，过滤生僻词）；
  （3）不属于"常见人名"或纯专有名词——常见人名按人口普查/SSA 综合名字榜取前 300 名；
      纯专有名词（不在 ENABLE 中的名字/地名）、月份/星期与粗俗词同样排除。
      `winter`/`west`/`fancy`/`fairy` 等"只是偶尔被用作名字"的常见词会被保留。
  最后去重并按字母序输出；各长度词条数随满足条件的候选自然变化（4-7 字母约 1100-1800
  词）。统一用"常见度下限"而非固定条数，避免短词因候选少而混入更生僻的词。
- `obscure_<L>.txt`：`--obscure` 困难模式的专用谜底池，取自"不常见但真实"的
  ENABLE 单词，要求 `wordfreq` 英语 Zipf 词频落在 `[3.0, 3.6)`，并同样剔除常见人名、
  纯专有名词、月份/星期与粗俗词。相比早先"`allowed` − `answers` − `excluded`"的
  catch-all 做法，该区间去掉了低频噪声（如 `huer`/`aaru`）、网络缩写
  （如 `gonna`/`dont`）与未登记的专有名词（如 `disney`/`england`），保证冷门局
  仍然"可解"。各长度词条数约为 609/1036/1512/1736。
- `excluded.txt`：上述屏蔽词与 `allowed` 的交集，运行时会从谜底池（含 `--obscure`
  冷门池）中剔除，但仍保留在合法猜测集合中（可以猜、但不会成为答案）。

替换词库时保持"每行一个小写单词、UTF-8、LF 换行"即可；`answers` 应始终是
`allowed` 的子集，`obscure` 也应是 `allowed` 的子集且与 `answers` 不相交，
以保证谜底总能被猜出；`excluded` 与两者都不应有交集。
