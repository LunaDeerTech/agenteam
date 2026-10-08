# embedselect01：原实际 FAIL，退休恢复完成，STOP

仅原批准 new-selection 一轮实际执行。Selection top 5.50s FAIL，首可见断言是 `platform_embedding_resolution_selection_test.go:229: resolution read material 5 <nil>`，位于最终 noSecretRead；此前10个子/嵌套测试终局PASS。共11个唯一 RUN 名。当前只记录事实，不凭5条计数归因，不改为常量5或删断言。

原fixture实际wait exit255/52.033s；driver的watchdog看到top FAIL后发送TERM。原adopted1次actualwait；外层实际wait exit1/112.548s。原996输入及执行输入前后同，7资源ID两次精确absent、owned两空；TCP57.453s内双清，75s预算不变。4个新PID1 containerd-shim属于非owned，原始记录保留且未信号/未声称wait。

原两cleanup均保留本轮runtime `go-build907913867`，所以原inner accepted=false/double_cleanup=false不变。root另授退休恢复后，仅在原direct/adopted真wait、81个原身份再次两空、路径位于本轮runtime、非symlink、唯一目录/同inode核定后精确rmtree该目录；实际exit0/0.868s并wait，runtime与81身份再两空。原8件SHA逐一保持，未触共享cache或未知路径。独立恢复不把本轮FAIL改写为通过。

原run/launch小件直接原位封manifest，未复制runtime/cache/source树。资源窗已交回，无重试或下一轮。v02历史gate仍会因原double_cleanup=false阻后继，不生成虚假disposition。接下来仅按root授权只读检查无材料读取断言的正式consumer作用域；任何源写、Go或业务资源等待另交。五新top其余四组、旧6/旧Summary3及独立A/B未执行，本卡/生产未接受。
