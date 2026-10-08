# read01 失败与退役独立审查

**read01 保留 FAIL；实际任务所有权范围内退役证据 PASS。归因为测试标题期待过强，本轮没有要求修改产品映射的契约依据。**

绑定作者冻结 handoff `21085e94…7b2200` 的93原件，逐项字节/hash与完整文件集合均相同。raw `c5cf12b7…9f0e8d`，result `d1b1377b…99c05b`；driver `bb666f55…b38e`、Go04、browser-v2 `9ca0cefb…3ccae`、本轮授权输入 `af1c94db…91fb72`。完整值见 [evidence.json](evidence.json)。

删除中直链真实 Resolve 返回409 `PROJECT_NOT_ACTIVE` / `not_committed`。D09 §32 与正式 Authority→Read gate 明确拒绝 deleting；HTTP零候选沿原typed error返回。D27 §3.2要求不发布删除中内容，允许不可用表示，却未要求所有409映射unavailable或固定“项目不可用”标题。ui-v1将本次409归read-error，初次导航无已发布详情，提供显式重读/返回列表。此为固定源码与真实响应的归因；未留DOM快照，不能把推导出的“项目信息读取失败”当独立DOM观测。

必修仅#21:465：匹配既有read-error显示，并核实际Resolve409/PROJECT_NOT_ACTIVE、零后续稳定ID Get/写请求、无form/ProjectNav及返回列表。保留合法deleting DTO受控不发布的覆盖。产品19源、API、parser、Session、Go和协议无需因本轮改动。root已另授browser-v3窄修；本报告不授重跑。

Go top17.21s FAIL，原builder/full-package命令114.299s、实际exit1。日志记录Node直接wait、proxy Serve/body全部join、Project准备服务join后释放ProcessGuard、default root join。外层direct实际wait＋4 adopted实际wait、watchdog实际join，forced/monitor/cancellation均0。7精确资源ID拓扑一致且两扫逐ID absent，owned PID、两类private runtime及新资源两扫空、Docker baseline不变；输入前后同。全TCP含TIME_WAIT补充观察在7.235s后两扫delta空，不推导连接所有权。4个新PID1 daemon shim不在owned表，未signal或冒称wait；不声称全机零。此次验证正常已注册cleanup处理浏览器断言失败，未注入任意Python启动/记录异常。

35 sidecar/34原body完整hash与run/input绑定通过，其中28个GET来自fixture正式准备、6个list及末次Resolve409。失败后第466行无form断言、第467行dotted导航及后续nonowner/name-reuse均未执行；verifyBodies也未到达，故**没有same-body schema/client、dotted或截图PASS**。完整read与后续真实接受仍未通过。

root于真实reader退役后恢复原3文件assets并保留测试53文件目录；这是资源窗结束后的授权恢复，不是本轮输入漂移。只以冻结v2与本轮before/after为依据，后续v3不回写原结论。本轮独审未启动资源、探针或停止动作，仅写当前scratch；本报告/证据现STOP。
