Model PG driver v02：G1 / G2 与专用 loader 差量、固定完整组合 STATIC PASS，STOP；尚非执行 READY。

固定 driver `79739d14689f2c907cda3a933762aefcde2358254ba25adb4f3ccf24dd0d9442`、launcher `d2be78b5e94ba62f102e480d06862cdf38ad769089875aa9e623e2dcac9f4f10`、stop `855de3ac05a901ccee0b979db21da10cc86c23d17d66055c35cf43ce4306104a`。manifest `87457c77…` 的 24 个原件 / 182898B 全部核 SHA/bytes；合必要旧源与固定引用共 40 个指纹。未执行 driver、launcher、Go、测试、资源或作者检查脚本，未读取当前 dist、host 或 cache。

G1 已闭合：实际 driver 入口在 source_input 前使用与 launcher 逐字相同的严格 true、只读模式、原 bytes、唯一 name/group、pending=[]、双执行源 SHA 检查。直接 driver 还必须找到本轮只读 intent/grant 副本，与实际父 PID/starttime、grant/source 精确一致；没有 outer launcher 的原绕过路径会拒绝。input-only 仍无资源路径。

G2 已闭合：两入口均检查 launches∪runs，旧 partial launch、只存在 inner PASS、outer 缺件或失败不再跳过。当前轮豁免仅作用于绑定正确且尚无 run/terminal 的正在启动项，实际创建顺序不会自阻断。前轮六原件绑定 outer wait/exit/accepted、inner SHA/wait/watchdog join/owned 两清理；完整原件中的失败仅可由当前 grant 明确绑定只读、精确六 SHA 的 root disposition 放行，原 FAIL 保留。缺原件、未 wait 或未清理不能被 disposition 替代。该分支核 owned 清理，不单独读取 TCP 原件；root 对失败继续的具体处置仍须保持原 TCP 观察范围，不能把 disposition 自身当成新完整退役证据。

新增 loader 独立于原 load_bound：按固定共享层递归 base、retain、remove、merge，并复制继承字典；当前层为 951→903→900→909 / 63 集合。只读 metadata 重建摘要 `8ce21289…` 与作者实际 909 文件门禁相同；未再次扫描 909 源或 355 外部组。原 source_input 仅切换这一调用。完整 v01→v02 补丁逐字同，19 个 accepted helper、v01 TopWatchdog/runtime_environment/actual_graph_input、7 组 14 top 和预算常量不变；没有业务源变化。

复核作者原件：73 个纯内存 gate 场景（6 allow / 67 block）与固定函数绑定，两个实际 Python 命令分别 0.164675s / 0.217235s、exit0、记录 actual wait；其范围不含真实进程/资源退休。原检查首轮 FAIL 保留，成功版本只补 fake filesystem 的 `/memory/runs` 父目录，driver/launcher 未因此变化。作者专用仓库门禁为 909 SHA / 63 集合 PASS；最终外部工具/运行时全门禁尚未产生。

prepared freeze 仍 false、pending 非空、offline_readiness=null、runtime fingerprints={}、无具体 run/group。后继还需封完整输入、固定 offline/list/helper/独审原件绑定与 root 明确的只读单轮 grant。本结论不接受实际七轮 PG、A/B、整卡、生产 consumer / serving / ExpectedDimensions / Nonchat / Invocation-root，三停止与 Jina 边界保持。原 v01 STATIC FAIL 不改写。

必要指纹与原件范围见 [result.json](result.json)，SHA `971cc44a6dc0c5e7746b0fc58793967d969d74df8eebff61500e5b3a1449d015`。
