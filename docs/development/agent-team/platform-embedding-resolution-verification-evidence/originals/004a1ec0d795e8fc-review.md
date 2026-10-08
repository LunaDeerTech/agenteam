# Model Selection 首轮失败、退休恢复与单查询候选独审

结论：**原 `embedselect01` 实际 FAIL 保留；独立的精确临时目录退休恢复 PASS；候选 `c43365bf…` 仅查询范围修正 STATIC PASS，无本差量必修。** 本报告不接受新编译或真实重跑，未执行 Go、SQL、资源或主机扫描，也未读取活跃安装/compile02。

原日志的十个子测试全部 PASS，最终顶层 `noSecretRead` 在 Selection:229 失败：全库 `secret.resolve` 为 5，SQL 无错；top 5.50s，不能据此判定 Resolver 产品读取了材料。固定源码显示构造身份时三次正式 Login 和两次邀请投递可以合法读取 System 材料，与 Model 共用正式 Secret 审计 writer。原件未保存五行逐行归属，所以这里仅认定原断言混入合法非 Model 消费者，不把五行实际来源补作事实。

候选相对冻结旧 helper `cd738f93…` 仅增加 34 bytes：`AND metadata->>'consumer'='model'`。独立正向替换、逆向移除均与整文件逐字节相等；count=0 与 SQL 错误即失败保持。正式 Model grant 为 `sc.Model`，两个 Secret 材料读取入口共用 writer，按 grant 的 consumer 构造经过校验的 typed metadata，再于同 Tx 写入 JSONB。该筛选覆盖同库 System/Project 范围、所有 actor 和历史/当前 lease 的 Model 持久材料审计；没有 project/actor/lease/time 限制、减常量或全库基线。五个作者 top 与 A/B 两处共用此 helper。它仍是持久审计行断言，不新增对回滚内材料读取尝试的证明。此前六 PG 的 STATIC 记录保留，本次实际失败补出了该测试范围缺口并用本候选闭合。

原轮固定 33 run/launch 原件共 330424 bytes 及 6 退休恢复原件已逐 SHA 核同。fixture direct actual wait 完成、exit255/52.033s；1 个 adopted wait 完成（exit -9）；outer 实际等待 driver exit1/112.548s。watchdog 已 join，但 selected-top complete=false；失败触发 TERM，forced-tail-action=0 不表示没有取消。81 个进程身份的两次 absence 不冒充 81 次 direct wait。精确 7 个资源 ID 均逐项两次 absent，owned 两空、baseline 相同、996 输入前后相同；TCP 57.453s 两次 delta clear 仅是补充轮询证据。4 个新增非 owned PID1 shim 未信号、未 wait。

原两次扫描仍有 `runtime/go-build907913867`，因此 **原 double_cleanup=false、accepted=false 永远保留**。独立恢复脚本仅在精确 81 PID/starttime 两次 absent 后，核唯一 task 目录的非 symlink、设备/inode/uid与安全删除能力，删除这一个 go-build 目录；command actual wait exit0/0.868s。之后 runtime 与原 owned 身份再次两空，原 8 个关键原件 SHA 不变。该恢复有限 PASS 不改原轮，也不绕过原 v02 的失败后继门禁。

后继只需新安装/离线凭据与单 helper 源指纹绑定，再按 root 独立授权重跑实际场景；本报告不授资源，不接受尚未完成的作者检查，不宣称独立 A/B、旧回归或整卡通过。旧失败与恢复、候选冻结及有限源码引用见 evidence.json，原件原位复用。**STOP。**
