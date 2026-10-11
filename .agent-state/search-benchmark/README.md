# 离线 lexical benchmark 历史输入恢复

任务引用 `origin/ai/search-benchmark` 的最后 tip 为 `55e9fd5e74a6b3a9d532bd2e1f52c7e44efe4faf`。其最新 current 明确关闭“固定语料＋可重复离线评分工具”这一完整独立任务，最后提交只更新关闭状态，没有复用为其他任务或协调分支。正式结果在 main `b2a7d0abef0b7ca0623c8e2a7c020658948848da`；9 个专属文档/源码/数据/独验文件已逐字核对该交付，台账另有本结果条目。

三候选 backend 实测、性能、版本/许可、hybrid RRF 与最终选型是明确排除的后继工作，不是该分支未完的实现范围。此处不恢复这些任务，也不扩展 D13 的完成结论。

## 可恢复的原 v1

[lexical-v1.patch](lexical-v1.patch) 从上述 main 交付源恢复语义返修前 `5dc38928ab2f1d3f7d066f25ae1f51d142243fc9` 的四个数据文件和作者测试文件。四数据也逐字等首数据提交 `0b6a14047162bb9d6102b02df8502f0066d21926`；评分器 `scripts/search-benchmark.py` 从原 v1 至交付始终逐字不变，SHA256 为 `580fc4d4f86a5ae7404afaa1280092680059b135719c8e7e6f9176484809bdfa`。

原语义问题包括通用 query 被 intent 暗限 scope 的漏标、q09 充分答案被压低、相邻条件/背景漏标，以及 startup-once 同族跨 split。`00ab72baab1370b7fe3060dbbb686935edb716bc` 的 v2 修正 19 处 grade/理由、q09 intent 与相关 family/split；交付前仅再归位判断来源状态。该补丁保留的是审查不接受的原判断，不把原 v1 标成语义通过。原语义审、评分实现独验及边界见 [main 正式卡](../../docs/development/work-items/d13-lexical-benchmark.md)。

为避免因每行修订号而复制整份数据，先只还原精确的 `dataset_revision` 标记，再应用零上下文补丁。没有重排 JSON、改正文或假造旧结果。以下恢复只在全新私有目录执行，读取固定 main 对象，不改当前产品，也不运行评分工具：

```sh
benchmark_copy=/tmp/benchmark-v1-private
python3 - "$benchmark_copy" <<'PY'
from pathlib import Path
import subprocess, sys

base = 'b2a7d0abef0b7ca0623c8e2a7c020658948848da'
root = Path(sys.argv[1])
root.mkdir()  # 已存在即拒绝，不覆盖
paths = [f'tests/search-benchmark/data/{name}' for name in
         ('corpus.jsonl', 'queries.jsonl', 'qrels.jsonl', 'dataset.json')]
paths += ['tests/search-benchmark/test_evaluator.py', 'scripts/search-benchmark.py']
for path in paths:
    data = subprocess.check_output(['git', 'show', f'{base}:{path}'])
    if path.endswith('.jsonl'):
        before = b'{"dataset_revision":"lexical-v2",'
        after = b'{"dataset_revision":"lexical-v1",'
        lines = data.splitlines(keepends=True)
        assert all(line.startswith(before) for line in lines)
        data = b''.join(line.replace(before, after, 1) for line in lines)
    elif path.endswith('/dataset.json'):
        before = b'  "dataset_revision": "lexical-v2",'
        assert data.count(before) == 1
        data = data.replace(before, b'  "dataset_revision": "lexical-v1",', 1)
    target = root / path
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_bytes(data)
PY
patch --batch --fuzz=0 -p1 -d "$benchmark_copy" < .agent-state/search-benchmark/lexical-v1.patch
```

恢复后五个文件的 SHA256：

| 文件（相对 `tests/search-benchmark/`） | SHA256 |
| --- | --- |
| `data/corpus.jsonl` | `946a0a56583f207fe6ce799e37f293d2d8ce947546fbd387d2f791cfb5082a28` |
| `data/queries.jsonl` | `5251142e36996d3ce76a118d68a27021de08e19aa9482124bafc67f0ae667fac` |
| `data/qrels.jsonl` | `8643f17c9e83499adb563ba9572fa8433f177432ee3429f24766a55643de3926` |
| `data/dataset.json` | `ba9efd5b3f7ce9b9780d47c0af30f1eb2f648b6a562fb4cf7e73b2fa42ab65e7` |
| `test_evaluator.py` | `22d42c884175fb3fc209f4fbd68e5b8dfb9bf455d369a76df9c0906ddb683d2e` |

本次已在私有临时目录实际执行上述修订标记转换及 `patch --batch --fuzz=0 -p1`，原 patch 进程退出 0；五文件逐字等目标 Git 对象，临时目录正常移除。补丁为 37,129 字节，SHA256 `80b10fed8f8a7147d0a374ee669d17216e87b4908d733afc61f4dd1555b9d368`。没有重跑产品、CLI、单元测试或后端评测。

## 不可恢复的原件与依赖边界

独验首次误把 q02 当作 zh 的夹具 FAIL，正式卡已明确其实际类别为 en，属于审查夹具问题。该控制文件首次进入 Git 时已是修后 `00ab72ba` 版本，与交付 main 逐字一致；旧误设版本未在该分支历史中保存，不能从修后控制倒造原件。必要修后控制仍在 `.agent-state/search-benchmark-review/evaluator-controls.py`。

旧 current 提及 `output/ai/search-benchmark/` 与 `output/ai/search-benchmark-review/` 的实际日志/导出；它们未入该分支 Git，本轮 main 保全区也未找到这些原目录。已有正式卡保留结果和失败限制，但不冒称可以恢复当次 stdout、完整临时 fixture 或全部原执行环境。本轮没有删除或改写这些缺失材料；保留旧引用也不能凭空恢复未提交原件。

本分支没有对应本地分支或注册 worktree，最后 current 明确无活命令/真实资源。现存协调索引未将其列为活跃任务；当前 Knowledge owner 已确认其实际配方不依赖该远端引用或旧树，只消费 main 组合。未发现后继 writer、运行依赖或该引用独有的未交技术范围。保存本目录后，已知必须保留的原 v1 源码差额可从 main 恢复；分支删除仍由 root 核对最新 tip 后执行。本目录不是永久任务索引或全树归档。
