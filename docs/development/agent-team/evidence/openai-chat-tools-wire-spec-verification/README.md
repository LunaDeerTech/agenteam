# OpenAI Chat tools wire 规格证据

对应[规格采纳报告](../../openai-chat-tools-wire-spec-verification.md)与[正式卡](../../../work-items/recovery-d09-openai-chat-tools-wire.md)。技术稿 `5c9656cb…` 独立静审 PASS；仅行政状态更新后以 `b2be334cd8a78b904878bdbc388a43805390f7b7` 提交，技术 §1–9 原字节不变。产品尚未验收。

- [source-manifest.json](source-manifest.json)、[字段定位](field-manifest.json)与[存储路径](source-files.json)绑定官方 `openai/openai-python@becc1d20eed83c1b8d85e15dc131a372d9dc7813`、SDK 3.24.0 的 21 个完整原件，共 104254 B。20 个限定源码以 `.py.txt` 保存，另有原 [Apache 2.0 LICENSE](LICENSE.openai)；没有完整 SDK、bare 库、缓存或二进制。未导入或执行 SDK。
- [commit-object.base64](commit-object.base64)无损保存 Git commit 对象，包括签名空白；解码后 SHA-256 为 `4a18fa5995a190f5cbff6f3313422abbae18861e401e2251ba73e7c8a59a9702`，Git SHA-1 为上述固定提交，与[已采纳 text 来源](../d09-openai-chat-wire-source/README.md)原对象相同。
- [公开 Git 获取命令](author/fetch-commands.json)、`author/git-*.stdout/stderr`、[原作者交接](author/author-handoff.md.txt)和原自查记录保留实际来源，不把作者自查当独立验收。获取关闭 credential helper/extra header/交互；未取用业务凭据。
- [原 rev1](inputs/rev1.md.txt)、[refusal 窄修](inputs/refusal01.md.txt)、[C0 窄修最终技术稿](inputs/c0-02.md.txt)和[采纳稿](inputs/adopted.md.txt)保留四个输入。`author/*diff` 与检查记录保留拒绝形状、C0 数字/深度/NUL 问题及行政变化；没有动态红例或产品测试结果。
- [独立原报告](independent/review.md.txt)、[66 项身份与接缝核对](independent/static-checks.json)、[最终差量和静态尺寸检查](independent/final-checks.json)及原检查脚本/索引均按原字节保留。原报告和脚本中的私有路径仅为当时记录；本目录正式入口与下列复原脚本不依赖那些目录。
- [固定依赖](baseline-inputs.json)精确绑定 ecd7337 的 14 个现有接缝；它们不同于卡 §8 的未来 14 个实施路径。[采纳定位](acceptance.json)绑定已提交卡、技术稿与独立报告；[原件映射](original-map.json)为 57 条来源记录、54 个唯一存储原件，三份重复输入只保存一份。

本目录已完整保存所选 21 个官方文件，离线复原不需要网络或 SDK 安装。目标须是仓库外不存在的新任务私有目录：

```sh
python3 verify_archive.py --restore-sources /task-owned/new-tools-source-copy
```

省略 `--restore-sources` 只核原件、SHA/Git blob、字段行、固定业务 Git locator 与技术稿一致性。若仍有原官方对象库，可加 `--git-dir /task-owned/exact-sdk-objects.git`，进一步按固定提交树复核每个路径；脚本从不 fetch、导入或运行 SDK。需要重新获取公共 Git 对象时，原 [fetch 命令](author/fetch-commands.json)是精确提交复现方式，禁止改用 latest。实际离线重建与检查见 [archive-checks.json](archive-checks.json)。

[SHA256SUMS.json](SHA256SUMS.json)覆盖本目录除索引自身以外全部文件；[whitespace-exceptions.json](whitespace-exceptions.json)只记录不可改写的原件空白。新增说明及脚本另作格式检查。来源身份、尺寸算例和文档检查不代表 Go/D04/PG/Provider 或完整 D09 通过。
