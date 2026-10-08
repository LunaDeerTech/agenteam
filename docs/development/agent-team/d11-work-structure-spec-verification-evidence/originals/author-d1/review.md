# D11 rev1＋D1（rev2 冻结）窄修；STOP

只改正式卡 §8.4 第339行中的预算含义：启动前 fresh 可用磁盘≥5GiB；owned/resource cleanup 后 supplementary host TCP delta 尾部双清观察总限≤75s，并明确不证明完整短连接轨迹或 tuple ownership。删除原 Memory 上限与全程 TCP 暴露窗误义；没有另设预算、路径或产品规则。

原完整独审 fdd314b3 为 FAIL/唯一D1，原 rev1 6d4f3335/原freeze及7个冻结输出均核同保留。正式卡遵仅§8.4授权，开头既有rev1标签不动；这里rev2指精确D1组合冻结版本。单hunk/单段行差量，实际在内存反向应用生成patch，59476B逐字还原6d4f3335；其它文字、18路径/六工程组、测试矩阵与45s/120s/6m均不动。

有限源只核BB driver v05 bb666f55的磁盘门槛与TCP尾观察及调用位置、完整rev1独审报告和原冻结引用；无全来源/Go图重扫。没有执行driver、filegate、Go/Node/Git、网络、数据库或任何资源，无产品改动。作者自查通过不冒最终独立组合PASS；请root交runtime复用完整rev1并仅审D1与最终组合。STOP。
