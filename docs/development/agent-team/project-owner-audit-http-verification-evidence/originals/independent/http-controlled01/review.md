# 独立 HTTP 窄补集普通/race PASS

绑定 candidate01 完整14冻结，通过15-entry overlay（14固定snapshot+私有1测试）隔离所有活动文件；无产品写入。实际普通图372包2305文件，race374包2311文件，各generated testmain1、extra0、local init/TestMain0。选中top没有socket/DB、os.ReadFile、Python或外部command调用，native环境关闭。仅 TestIndependentProjectAuditHTTPControls，普通/race各1top2sub。

1. Body.Close主动触发cancel并等待AfterFunc第一个expired write setter已持有，再panic；finish的第二expired setter可返回且到达检查点，此时handler仍未返回。放行原callback后实际done，Close恰一次、业务读一次、零Header/Write。无条件Cleanup cancel/release/actual done，Fatal路径不逃逸。
2. 成功响应已write/flush后，reset read setter panic仍运行write reset，统一ErrAbortHandler；仅一次成功Header/body，不二写Problem，Close一次。此为受控writer边界，不声称任意坏wrapper全root兼容或真实socket退休。

8条格式/两图/两编译/list/两运行均45s外限、40s test、offline readonly p1/Go1.27.1/subreaper/direct actualwait、双owned空、输入前后同，未触发强制清理，无首红。无native/PG/真实root资源；作者实际窗保持独占。固定query普通/race与schema40补集另见原索引，不重复无关矩阵。结果冻结停止写入。
