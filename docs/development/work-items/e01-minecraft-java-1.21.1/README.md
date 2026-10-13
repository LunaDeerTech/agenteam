# E01：Minecraft Java 1.21.1 来源目录候选

这是 [E01 r1](../platform-game-acceptance.md) 的实施前资料准备，**不是完整游戏清单、最终冻结包、计分分母或游戏验收**。D28 及平台前置仍未完成；本目录没有启动游戏实现、平台任务或模型调用。Minecraft Java Edition 1.21.1 是本次有据候选，正式选定和冻结仍按 E01 的前置及独立完整性核验执行。

## 实际取得的来源

2026-10-13 获取并保存 8 份轻量文本来源，原始正文、URL、字节数及 SHA-256 在 [sources.json](sources.json)。没有下载 client/server JAR、纹理、声音、世界或 NBT 正文。

- Mojang 的固定 [1.21.1 版本对象](https://piston-meta.mojang.com/v1/packages/499680d659b7a0ccb2fa9a809c345c5baf2ace7d/1.21.1.json)标明 `release`、发布日期 `2024-08-08T12:24:45+00:00`、Java 21。对象正文的 SHA-1 已与官方 URL 中的对象标识核对。
- [官方发布说明](https://www.minecraft.net/en-us/article/minecraft-java-edition-1-21-1)称它为修复严重漏洞的正式 hotfix，并说明 1.21 客户端可连接 1.21.1 服务端。本候选包含现代生存及进程系统；选择它是因为有稳定版本及可定位的数据来源，不以极早版本减少原版内容。
- 社区维护的 [mcmeta](https://github.com/misode/mcmeta/blob/3d63d61bcb7421a0d8ddda656bc851cc178067dd/README.md)说明数据来自 Mojang 数据生成器及公开 API，并明确自己不是官方产品。此方法声明可作来源说明，**不等于本轮已用原版重新生成或独立验证内容**。
- `1.21.1-summary` 固定为 `7cffbcdad5fa36f22acef6bd4b869ecad4ca3fd9`；保存完整 summary 树索引、版本对象及 `registries/data.min.json`，后者 Git blob 摘要与树中记录匹配。
- `1.21.1-data` 固定为 `aacd2a457333b258d044a642f38270c8cddbc628`；保存未截断的完整 Git 树索引，每个文件的路径、大小及 Git blob 标识可定位正文。这里只采索引，不下载这 6,801 个文件。
- Minecraft Wiki `Java Edition 1.21.1` 页面编号 `266769` 的版本元数据实际返回 revision `3801512`。随后请求 revision 正文收到 HTTP 403；因此仅存版本定位，**没有把它当作已读取的手册、行为或完整性依据**。

## 实际目录和差异

[catalog.jsonl](catalog.jsonl) 有 **20,672 个来源概念记录、27,414 条来源关系**；[summary.json](summary.json) 给出分类计数和全部差异。汇总来源含 **136 类、20,613 条 registry 记录**；独立的 data 树索引含 **6,801 个文件**，其中 5 个为来源元数据。这里的“完整”只指已保存来源自身的全部 registry 条目和全部 data 树文件均有映射，绝不表示完整原版行为已覆盖。

比对发现 registry 的 `worldgen/structure`、`tag/worldgen/structure` 和实验交易平衡包的结构标签都是空数组，而同版 data 树实际分别有 **34、13、7** 个定义。目录保留这 54 项并标出其 data 来源，例如 `minecraft:trial_chambers`；没有把空数组解释为原版不存在这些内容。summary 树还包含旧格式及历史实验分类，不能把其中所有路径直接认作本版有效内容；本目录仅使用所选版本的 registry 正文和 data 树生成内容记录。

每行 `id` 为稳定来源概念 ID，`category` 和 `name` 保留原分类与资源名称，`sources` 保存 registry 数组下标和/或准确 data 路径。两类来源中相同的类别、名称合并为同一记录；实验包使用独立类别保留，源元数据显式列为 `source_metadata`，没有静默丢弃。映射关系可以双向查询。

**来源概念 ID 不是计分叶子 ID。** 方块与对应物品、配方与配方解锁进度、材质与模型、内部类型与可玩行为均不能直接按条目数计分。所有行为验收、去重分组、正整数权重、关键门槛和系统分母仍待制定及独立核验。`summary.json` 的 `scoring_leaves`、`denominator`、`coverage` 为 `null`，不把尚未建立的分母写成 0%。

## 复用入口

从项目根目录运行离线来源投影检查：

```sh
python3 docs/development/work-items/e01-minecraft-java-1.21.1/catalog.py
```

它核对正文长度/指纹、版本与发布日期、未截断且无重复路径的来源树、registry blob 身份、重复条目，以及所有来源逐项投影后的目录和汇总是否一致。少一项、多一项、重复映射或修改汇总均明确失败；不访问网络、不运行游戏或计算覆盖率。

```sh
python3 docs/development/work-items/e01-minecraft-java-1.21.1/catalog.py --source data:data/minecraft/worldgen/structure/trial_chambers.json
python3 docs/development/work-items/e01-minecraft-java-1.21.1/catalog.py --content mcj/1.21.1/recipe/minecraft:acacia_boat
```

`--collect` 仅用于首次获取有界文本来源，已有 `sources.json` 时明确拒绝覆盖。要发布后续候选修订，保留本版及差异，按正式 E01 规则重新核验；不要删除现有证据后静默刷新。脚本只需 Python 3 标准库。本次实际采集及作者离线检查已运行通过；后继非作者来源投影复核也已通过，覆盖保存来源的全量映射、摘要/版本/Git 树一致性、双向查询及八项针对性拒绝检查。该限定结果不替代独立全游戏内容、行为粒度、权重和关键门槛审查。

## 尚缺的冻结材料

可机器读取的缺口见 [candidate.json](candidate.json)。本轮已经落实来源目录，不以新增泛化方案代替剩余工作：

1. 数据生成器目录没有穷尽生命/饥饿、移动/碰撞、红石时序、繁殖/AI、交易、战斗、死亡与重生、多人、保存恢复等可观察行为。尚无逐版本手册/行为条目全集及与目录的逐源覆盖图；Wiki revision 正文未取得。需要实际来源补齐，不能从当前拟实现功能反推全集。
2. 尚未将来源概念归并为粒度一致且不重复的可观察计分叶子，没有逐项验收条件、统一权重、全系统门槛或连续试玩路线。可选实验包及各种游戏模式保留为待核适用性；没有据此排除困难内容，也没有把生存子集称为整个 Java 版。
3. 未完成独立原版来源交叉核验、目录完整性/粒度/权重审查及主线程最终冻结。mcmeta 的两个视图属于同一个上游，二者对照不是独立原版复现。
4. 实际平台前置、游戏环境锁定、Project/Task/Execution/审核/Artifact 关联、实现与试玩证据均未建立。当前目录不能用来登记通过项或宣布 E01 开工/通过。

后续工作应复用已固定目录及 54 项具体差异，补实际行为来源与 E01 冻结包；本次不重写 E01 r1，也不产生游戏实现或覆盖率结论。
