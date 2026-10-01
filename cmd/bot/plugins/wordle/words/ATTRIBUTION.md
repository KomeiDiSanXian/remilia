# 词库来源与授权

本目录下的 `allowed_*.txt` 与 `answers_*.txt` 由以下两个公开数据集生成：

- 英文单词表：[dwyl/english-words](https://github.com/dwyl/english-words)
  （`words_alpha.txt`，Unlicense / 公有领域）
- 词频表：[hermitdave/FrequencyWords](https://github.com/hermitdave/FrequencyWords)
  （`content/2018/en/en_50k.txt`，MIT License）

生成规则：

- `allowed_<L>.txt`：长度 L、仅含 `a-z`、且出现在 `en_50k` 词频表中的单词（去重、排序）。
- `answers_<L>.txt`：`allowed` 中词频最高的前 1500 个单词（去重、排序）。
- 生成时排除了一份人工维护的粗俗/攻击性词表。

替换词库时保持"每行一个小写单词、UTF-8、LF 换行"即可；`answers` 应始终是
`allowed` 的子集，以保证谜底总能被猜出。
