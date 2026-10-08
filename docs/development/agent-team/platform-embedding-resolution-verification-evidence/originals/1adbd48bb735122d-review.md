# embedselect02 原件独立复核

**有限 PASS / STOP，仅接受本轮 new-selection 的实际结果与退役证据。无必修。**

原 raw 恰有 1 个顶层与 10 个嵌套测试，全部 PASS；Selection 为 5.99s，Model package 为 7.024s。其余 package 的 no-tests 输出不计业务覆盖。fixture actual wait 为 exit0 / 53.455s，outer actual wait 为 exit0 / 108.939894s；watchdog 已 join、无遗留 active/失败，adopted wait 为 0。83 个观测进程身份不计作 83 次 wait。

两次原 cleanup 均显示 7 个精确资源 ID（4 container / 3 network）逐项 absent，owned、runtime、remaining-new 均空，baseline 相同。TCP 尾部 52.430464s 后两次 delta clear，未发信号；它仅是补充主机轮询证据。记录观测 Node/browser 均为 0。4 个新增非 owned PID1 shim 仍单列，未计入 wait/join；194 个历史 zombie 未触碰。该代 prior_runs 为空，不扩大跨代 ID 唯一性结论。

root grant、inner frozen 与 outer grant 副本逐字节同为 03068c0b；单轮权限仅 embedselect02/new-selection。driver 79739d14、launcher d2be78b5 与执行副本绑定；880a source delta、安装/compile STOP 与当前唯一 fixture 指纹均指向 c43365bf。996 输入前后记录逐字节相同、无 missing/mismatch/set-change；本次只核原件，没有另跑 filegate。

原 embedselect01 FAIL / cleanup=false 及独立恢复继续引用 004a1ec0，未重审其 39 件，也未覆盖原结论。c433 的持久 Model-consumer 审计断言范围沿用已审来源，不扩展为回滚内材料读取证明。余下六个作者 PG 轮、独立 A/B、整卡及生产绑定均未据此接受。本次无 Go、driver、SQL、资源、网络或当前主机/cache 检查。
