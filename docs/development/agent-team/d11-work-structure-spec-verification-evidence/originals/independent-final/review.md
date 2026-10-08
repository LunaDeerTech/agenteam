# D11 rev1＋D1 最终组合 SPEC STATIC PASS；STOP

固定正式卡与 rev2 副本均为 c07c7112a5fd77bd475cd843fbffc9680b030b626550632f2999d849476bf07c（59597B）。复用完整 rev1 独审 fdd314b3，仅复核 D1 单段差量与最终组合；没有剩余规格必修。

独立逐字比较确认只有 §8.4 第339行替换，增加121B。反向替换精确还原59476B原卡6d4f3335。修正准确区分启动前 fresh 可用磁盘≥5GiB与 owned/resource cleanup 后补充 host TCP delta 尾部两次清空观察总限≤75s，并保留“不证明完整短连接轨迹或 tuple ownership”的边界。它没有新增内存上限或全程TCP暴露预算。原45s离线、120s每top含setup/body/cleanup、package6m及精确资源退役保持。

其余文字逐字不变，沿用 rev1 完整契约审查：六命令/四读/Lookup、同Store活Tx/完整锁union、当前授权→历史→新mutation、rank与分页、typed Outbox producer双阶段gate、Unknown边界、18路径/迁移00021预留、6新PG/7旧pure/5旧PG与两个不同构造独验，以及明确未绑后继范围。卡首rev1标签在§8.4唯一授权内保留；本结论称rev1＋D1组合，行政接受另由root处理。

原rev1卡、原freeze和唯一D1未通过报告保持原字节。核10个必要固定引用，无38来源/依赖图或全文重复审查。只进行文档静态比较与哈希核验，未运行Go/Node/binary/list/SQL/driver/filegate/网络/Git或资源，未写仓库和产品，无未完成命令。SPEC通过不代表产品实现、编译或真实PG行为通过，也不授资源执行。STOP。
