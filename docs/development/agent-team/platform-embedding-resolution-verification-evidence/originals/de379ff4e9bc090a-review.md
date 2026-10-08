embedreplay01 / new-replay：有限 PASS，STOP。

本结论是作者真实 PostgreSQL 轮次的独立原件复核，不是本人独立 A/B 实跑。固定 inner f8793ba7、outer 6ce9e875 与冻结 replay b796560e、当前 fixture c43365bf 已核合；无必修。

原 raw 的 45 RUN/PASS 名称逐项闭合：1 top＋2 purpose 组＋42叶，其中28项是 same-Call 组合。两 purpose 的历史 snapshot/lease 不因当前选择、停用、endpoint或正式删除被重新绑定；新的 Authority 与正式新 Session仍取持久结果，当前 strict facts、权限、显式版本及 canonical ref 的错误优先序仍生效。consumer mapping 改变不承诺 Model plan_version 递增。受控 released SQL不复活 lease；末 consumer=model noSecretRead 实际通过。新 Authority不等于进程重启，fixture subject/operation/lease直接变更不是生产 consumer 或 release/material API。

top 12.57s、package 13.606s、fixture 65.477s、outer 119.96749017100228s，后者接近120秒的事实保留，未改变原预算。fixture及outer actual wait/exit0，watchdog实际joined；0 adopted、83 observed不能写成83 wait。精确4容器＋3网络各两次 absent，owned/runtime两空、基线不变；TCP75s预算内51.455966s最后两扫无新增行，仅是补充host轮询。4新增非owned PID1 shim不wait或处置，历史214也未动。996输入前后相同，7 IDs 与记录的先前轮次不同。

没有重跑任何检查或资源，也未读取活动 Unknown。无生产 Knowledge/Memory接入、外部Provider、serving/维度/Invocation、独立A/B、旧包无测试路径行为PASS或全机零进程外推。原 embedselect01 FAIL及其原 cleanup=false不因本轮改写。
