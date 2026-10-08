# D11 Work structure rev1 — 完整 SPEC STATIC，需 D1 修订；STOP

固定卡 SHA 6d4f33356ece71a3f2a5d69d6155bb1d1284e8455c9bb2e272a5483c85457527（59476B）全文审查完成。仅一项规格必修 D1，未发现第二项产品契约必修；当前不发整体 SPEC PASS。

D1 位于 §8.4（固定卡第339行）：“保原基准”后的“fixture Memory 上限≤5GiB”和“owned TCP 暴露总窗≤75s”都改变了既有含义。已接受 BB driver v05 第490–494行实际检查 task 根目录可用磁盘 `shutil.disk_usage(ROOT).free >= 5*(1<<30)`；不是内存上限。第419–438行的75秒从 `retire_tcp_observation` 开始，第657行在 owned/resource 双清之后调用，是补充 host TCP delta 尾部双清观察，不是运行全程 TCP 暴露期限，也不能证明所有短连接或 tuple 所有权。Go/verification 两技能未给出所谓继承内存上限。

必要窄修：准确保留 fresh 可用磁盘≥5GiB 与清理后 TCP 尾观察≤75s，并保持离线45s、top120s含setup/body/cleanup、package6m及实际精确资源退役。若另有意新增 Memory 或全暴露限制，应单独标为新工程规则并给执行依据，不能冒原预算。无需改变产品语义、18写域或测试矩阵。

其余组合可成立：六命令/四读/Lookup/同Tx placement；真实父子/typed SprintID与五表；rank固定整数算法、no-op、rebalance旁观版本与generation；当前Session/Owner/Read→命令历史→新Mutate；planned/final及最多三轮重规划、原identity只读Unknown确认；Work真实command/canonical producer与Project精确双阶段gate；第一次完整union、原始512计数、同Store活Tx、不在Validate补锁；no-op与历史Activity/Event边界均明确。构造顺序可先建Work Authority/typed catalog再Outbox Seal和Service，不需循环nil依赖或新增公共接口。

18路径/00021仅预留、6新PG、7旧pure/5旧PG的精确声明与两个不同构造独验要求已核；源码声明不是已执行发现或通过。Task membership、删除正向、Sprint lifecycle/pointer writer、Work整体清理、Agent/Execution/Dispatch与生产root保持后继未绑。已有initializer只用于明确持久隔离组合，不冒Skills/登录或Stopped Object运行。

复用准备 c9864dc6/2d53d94c；仅补必要固定接缝和预算依据，无全38源/Go图重扫。未改正式卡/产品，未执行Go/Node/binary/list/SQL/网络/Git或任何资源，未读活动前端，无委派。原rev1/作者自查和历史原件保持；后继只需D1精确差量＋最终组合复审。
