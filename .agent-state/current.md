# Skills P2 独立补集恢复点

- 目标：未参与 Skills P2 实现者验证四口当前确认与真实 D05 Package 的当前权限、取消和实际返回接缝。分支 `ai/skills-p2-independent`，基线 `eaad50fdb08e248b850a65c74f49d3aabf73b682`；唯一 writer 为 Variables agent。作者已确认本域生产自 d0a16242 未变并继续冻结。
- 范围：两个有限纯 top；新真实 `TestSkillIndependentP2ConfirmationAndPackage` 的四子，必要既有 root-chain 精确 selector 增量另审。只新增测试/独立资产，默认不改作者生产、旧 tests、锁文件或迁移。具体判据见 `.agent-state/skills-p2-independent/README.md`。
- 两个纯 top 已实际通过：`python3 .agent-state/skills-p2-independent/pure.py`，session21592→238565 actual exit0，race 1.068s，2top/7sub。确认覆盖当前拒绝、原 Unknown、弱锁、foreign issuer、原 request；Package 覆盖 Read/Close 两种放尾顺序、原错误与实际返回前不 join。实际生产 Service/P1 经只读 Go overlay，Store/Project/Object 为明确 doubles；不属于真实 SQL/D05/PG 证明。两资产为 `.agent-state/skills-p2-independent/{pure_test.go,pure.py}`，gofmt/Python syntax/diff-check cbfaf6 actual0。
- 真实四子测试、业务 binary、exact selector 接缝尚未写完/编译/执行；没有本树在途命令或真实资源。Variables Authority03 的08/dist03为另一固定输入，不在本树重建或改动。
- 可复用作者60950/58518/39205原持久初始化/回滚/COMMIT恢复，96753当前Stop，70196真实D05发布/EOF+Close，61543迁移/Admission/Owner矩阵，各保原产品与受控端口边界；不把这些作者证据叫独立补集通过，原4315 FAIL保留。
- 真实场景上游Human/Session/Project/Creation/lifecycle仍为明示规范seed，nil Object Runtime；不称Login/Create/BeginDelete/foreign死亡。Cleanup/Purger/完整participant/root与D05新bounded cleanup/00028实际发布不属于本次，Runtime join停止项不恢复。
- 固定离线Go `/workspace/toolchains/go1.27.1/bin/go`，GOTOOLCHAIN=local/GOENV=off/GOWORK=off/GOPROXY=off/GOSUMDB=off/GOTELEMETRY=off/GOMAXPROCS=2，readonly `GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod`。只用既有本人 `GOCACHE=/workspace/agenteam-project-variables-ui/output/ai/project-variables-ui/implementation/gocache`，不另建大cache；临时输出与运行材料放本树 ignored `output/ai/skills-p2-independent/`。纯控使用上述原 cache；后继业务编译及真实资源分别安排，真实必须root freshgrant及原资源完整尾。
