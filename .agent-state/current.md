# Model UI 正式 main 集成检查点

- 树：`/workspace/agenteam-model-ui-delivery`；分支 `ai/model-ui-delivery`；正式基线 `3cea6076bb01693ead2755826826d189626aa3aa`。root 已从原冻结组合精确带入五测试路径、67 个必要恢复文件及卡；不得整树覆盖 main。
- 负责人 `/root/model` 拥有这些限定源、本文及 frontend README；Git／最终交付由 root 执行，global tasks 不在本写域。当前无 PG／browser／socket grant。
- 原组合已接受：authority19 52868 完整 PASS（Go39.54s／outer128.596s、13checks、31attempt／29EOF=typed=schema／2预期不完整、实际Wait／七资源与目录／TCP双清／inputs同一）；现新6/6、旧14/14、独立A/B按版本组合通过。前18轮 FAIL 保留于卡。该结果属于旧 delivery11c／迁移≤22／旧冻结资产，新 main 含后续迁移和 Variables／Work 装配，尚未完成本组合验收。
- 原树 `/workspace/agenteam`、旧 `/workspace/agenteam-delivery`、fixed54e及0906／53f备份和历史证据保持原位冻结，不将旧二进制作为本树新结果。

## 新树实施

- root 已确认保留 main 五个 Variables 契约／Audit 路径；Model25web与共享 UiDialog／UiPopover／useLayer、锁文件及Model schemas全与 main 相同，详见 `.agent-state/model-ui-recovery/integration-brief.md`。
- 四 driver 将 `DELIVERY` 从旧绝对目录改为其实际 `ROOT`：recovery/run-owned-top.py、recovery/fixture-go.py、regression/run-owned-regression.py、independent/run-independent.py。其余源码逐字96934da4，selector／预算／资源／Wait／EOF／finished／身份门槛不变。
- 新 `.agent-state/model-ui-recovery/delivery-path-controls.py` 实际c22872 exit0：四实际源绑定正控、20个旧树／其它树／重复赋值／额外改动／父目录负控；仅AST和git show，不import driver或启动资源。五源已冻结交Runner窄审。
- 离线依赖／缓存、针对性Audit/menu单元、TS／资产构建、account race／精确发现和路径窄独审现均已完成，实际结果见下节；下一申请新main authority和受VariablesAudit变化影响的必要ProjectAudit真实子集。不机械重复旧14。

## 构建与磁盘准备（已完成离线准备）

- 固定 Go `/workspace/toolchains/go1.27.1/bin/go`；显式 `GOTOOLCHAIN=local GOENV=off GOWORK=off GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOFLAGS='-mod=readonly -p=2' GOMAXPROCS=2`。
- 可复用原 Model 独占 GOCACHE `/workspace/agenteam/output/ai/model-ui-recovery/go-build`（1.3GiB），只读共享 GOMODCACHE 同目录 `go-mod`（197MiB）；本线不并行两次可写cache编译。root已明确授权本线独占复用，避免另建空缓存复制1.3GiB。
- 两套锁一致的 node_modules 已以hardlink共享不可写包文件，明确排除.vite/.vite-temp/.tmp/.cache/.vitest/coverage/.nyc_output；Vite／Vitest新缓存目录为本树独有。未安装／下载／执行postinstall或chmod共享inode。MinIO仅只读link原已验证固定产物。
- 初查available5,935,796,224B，距5GiB约567MB；新account／四helper／web及native输出预计105–115MB，增量Go cache/tmp另估150–400MB但不是硬上界。Vars07编译实际退出后，经root协调才启动本树唯一Go增量构建。
- 真实启动仍必须fresh同进程statvfs≥5GiB与root独占授权；原磁盘观察不是后续门槛证明。构建成功也不等于main真实通过。

## 本次离线实际结果与冻结

- Runner路径窄审d496e4/7b7bd3实际0，无must-fix：四ROOT实解析本新树、12覆写负控；原selector/预算/resources/gate/argv逐字不变。未执行driver main。
- Audit/client/metadata/menu三个实际web spec，74234 actual0，148项；Vue应用类型3729 actual0。两browser spec首TS28518 actual2为命令ES2023缺现有String.isWellFormed声明；仅改lib ES2024后62553 actual0，无源改动。限定Prettier94161 actual0；两Go gofmt无输出。
- 新树 account race构建36615 actualWait0；072e6c精确六新top，6372cb精确两ProjectAudit旧top，均actual0。文件 `output/ai/model-ui-recovery/account-delivery.test` 为58,286,985B，SHA256 `d8c1a058e73d06c95cd121c023f19d435997851d2d19ca229ba8eef38f4a7af3`。它由main3cea+本fixture实际构建，不是旧54e2复制。
- 四fixture helper98176顺序构建、各actualWait0；Vite63221 actual0，286modules/64assets，写本树私有dist；native b386c1 actual0，7modules/89.09kB。原web/dist未写。
- d7e52b实际新资产的公开singleton+AccountFailure精确导出解析通过；只提取原driver输入表达式预飞26source/6binary/5formal/64asset，路径均新树实际文件。Python schema依赖可用，无main/网络/资源执行。
- 构建结束可用5,550,333,952B，私有Go tmp空；此非未来真实启动门槛。全部构建/检查actualWait已终态，没有活Go/Node检查。
- 现freeze全部限定源、本文、卡与新增README段供root安全checkpoint。拟真实scope仍authority＋audit-authority/audit-navigation（由root最终定案），必须freshgrant。未跑新main业务，旧证据不冒新组合PASS。
