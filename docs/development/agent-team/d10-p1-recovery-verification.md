# D10 P1 恢复独立验收

2026-10-05，`verification_worker` 独立审查。**结论：固定提交 `8872110099c84cf0600bb5b62cdcd6c0c6c843e3` 的 P1 纯契约、不可变包和真实 builtin 通过本次验收，未发现阻断缺陷。** 本实例未参与该实现；本结论不代表 D10 模块或生产初始化链路完成。

## 固定输入与授权边界

- 输入为 [D10 主卡](../work-items/d10-skills-initialization.md) rev1、[设计](../work-items/d10-skills-initialization-design.md) rev1（P1 以 §9 为准）、[Skills 架构](../../architecture/agent-skills.md) 和 [D01 资源契约](../work-items/d01-contracts/resources-skills.md)。已读取仓库规范、团队流程、开发计划、台账和 [验证技能](../../../.agents/skills/agenteam-verification/SKILL.md)。
- 开始时 `main = origin/main = 8872110`，工作树干净。该提交新增五份 Go 生产源、五份测试、真实 `SKILL.md` 与两份规格文档；没有旧域、依赖锁、迁移或应用根改动。
- [输入指纹](evidence/d10-p1-recovery/inputs.json) 保存 11 个实现/测试/builtin 文件、`go.mod`、`go.sum` 和两份规格的 SHA-256；文件自身 SHA-256 为 `d0126373a686ff6cb4ebf27808984ac23c26f99090222b394db4d2769da5d536`。另保存 [实际编译依赖指纹](evidence/d10-p1-recovery/compiled-inputs.json)，涵盖 15 个非标准库包的 78 份源码/嵌入文件。
- 收尾时主线程已将无关恢复文档提交为 `a9cf0de222ce846f40784472e3d3dbce43a0d3e5`。逐份比较后，实际编译的仓库源码仍与固定 `8872110` 完全相同，未因 HEAD 文档变化重复测试。
- 本实例仅新增本文和 `evidence/d10-p1-recovery/`；未修改产品源码、原测试、旧主卡或台账，未执行 Git 写操作，未使用 Docker、数据库、MinIO、共享 fixture 或产品服务。

旧 `/tmp` 作者证据已经丢失，本次没有将旧完成声明作为通过依据。首次本地测试因默认模块缓存缺少已锁定 `golang.org/x/text v0.41.0` 且 `GOPROXY=off` 而 setup 失败，两个包均未执行测试；[原失败输出](evidence/d10-p1-recovery/initial-setup-failure.log) 保留。随后在本任务独立模块缓存从正式 Go proxy 下载同一锁定版本，校验通过，未更改 `go.mod/go.sum`；[依赖证据](evidence/d10-p1-recovery/dependency.json) 保留版本、校验和与来源。这是环境恢复，不是产品测试失败。

## 实际命令与结果

工作目录 `/workspace/agenteam`；工具链 `/workspace/toolchains/go1.27.1/bin/go`，实际版本 `go1.27.1 linux/amd64`，`CGO_ENABLED=1`。正式测试统一使用以下环境，三个缓存/临时目录均归本任务独占；测试期间禁止依赖下载：

```sh
export GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off
export GOMODCACHE=/tmp/agenteam-d10-p1-recovery-r53114d7/modcache
export GOCACHE=/tmp/agenteam-d10-p1-recovery-r53114d7/cache
export GOTMPDIR=/tmp/agenteam-d10-p1-recovery-r53114d7/tmp
```

下表 `go` 代表上述绝对工具链路径。精确 argv、环境、exit code、原始 JSON 日志 SHA 和保留日志 SHA 见 [results.json](evidence/d10-p1-recovery/results.json)。保留的 `.log` 是按原顺序提取的 Go JSON `Output`，只去掉 JSON 信封；未省略测试失败。完整原始 JSON 留在本任务 `/tmp`，重跑不依赖该临时目录。

| 命令 | 实际结果 | 持久日志 |
| --- | --- | --- |
| `go test -count=1 -json ./internal/central/skill/...` | exit 0；13 顶层、16 子例；两包 0.004s / 0.041s | [unit.log](evidence/d10-p1-recovery/unit.log) |
| `go test -race -count=1 -json ./internal/central/skill/...` | exit 0；同一 13 顶层、16 子例；两包 1.017s / 1.196s；无 race 报告 | [race.log](evidence/d10-p1-recovery/race.log) |
| `go vet ./internal/central/skill/...` | exit 0，无诊断 | [vet.log](evidence/d10-p1-recovery/vet.log) |
| `go test -overlay=/tmp/agenteam-d10-p1-recovery-r53114d7/overlay.json -race -count=1 -json -run '^TestRecovery' ./internal/central/skill` | exit 0；7 项独立探针，5.529s；无 race 报告 | [probe-race.log](evidence/d10-p1-recovery/probe-race.log) |

独立探针通过 Go overlay 加入一个虚拟外部测试文件，调用公开 API；没有把测试写入生产源码目录。首次六项探针已通过；增加最大合法包正例后，最终七项在同一冻结探针源码上全部重跑通过。[完整探针源码](evidence/d10-p1-recovery/probe_test.go.txt) SHA-256 为 `e360642615f5e614e85e23d2dddbb1c32d77516f5972b96bfa8575c04f571602`。

在具有相同冻结源码的检出目录内可直接重跑：

```sh
python3 docs/development/agent-team/evidence/d10-p1-recovery/reproduce.py \
  --go /workspace/toolchains/go1.27.1/bin/go
```

[重跑脚本](evidence/d10-p1-recovery/reproduce.py) 先核对源码指纹及 Go 版本，创建新的专属 `/tmp` 缓存和 overlay，下载同一已锁定依赖，再串行执行四项检查并留下日志；不创建 fixture，不修改业务源或依赖锁。脚本自身通过 Python 语法检查；本次实际行为证据来自表内已执行命令，没有为检验脚本再次重复整套测试。

## 审查与独立场景

| 边界 | 核对及观察结果 |
| --- | --- |
| 真实 builtin | `builtin.add-skills.v1`、revision 1；正文 2473B，SHA `a5f2416d9531ca0d450d97fd9d7064187c3d865573b9b20d874d146f1ee9663d`；包 2587B，SHA `a67cef2cf755baa48880ea1727444ab060ac6237e5505e99081e868c91f1dcf1`。文本明确已有权限、安装与分配分离、固定 revision，不授予缺失能力。构造执行两项指纹校验。 |
| ZIP 格式与攻击面 | 用直接写固定二进制字段的独立编码器构造合法 ZIP，与 Build/Parse 互证；912 个逐 bit 的 local/central/EOCD 头变异全部拒绝。真实 129 / 65537 条中央目录伪装为 count=1 被拒；源码确认实际目录界限检查先于标准 ZIP reader 分配。原测试另覆盖 CRC、声明大小、前缀/尾部/拼接、压缩方式、链接/设备、额外字段及目录。 |
| 路径与正文 | 同名/大小写、NFC、full case-fold、文件祖先冲突，以及越界/绝对/反斜杠/冒号/非法 UTF-8 均拒。探针直接将恶意路径编码入容器，证明 Parse 同样执行检查。合法分解 Unicode 与无末尾换行保持原字节，空附件允许；原测试覆盖正文 UTF-8、CR/NUL 和固定 frontmatter。 |
| 数量、体积与预算 | exact 256KiB 入口、8MiB 单文件、32MiB 总正文的输入合法，32MiB+1 拒绝；128 文件、32MiB 总正文、附件路径均达 1024B 的完整包 build/parse 成功，ZIP 为 33,824,294B。race 下本机 build 664ms、parse 1.893s；这是此次机器观察值，不是普适性能保证。32MiB 工作中取消不返回有效包，过期 caller deadline 保留；源码使用 caller 与 2s 最短预算，没有业务后台工作。 |
| 不可变与 JSON | 输入数组、manifest 投影和包字节使用副本；失败的通用 JSON 解码不能覆盖 closure。探针验证重复/错误大小写/未知字段/非规范整数/尾部 JSON 拒绝，以及 fmt、JSON、slog 不暴露正文。manifest 明确投影只含路径、媒体、字节数与摘要。 |
| 精确读流与所有权 | scope、ID、媒体、大小、SHA、状态、版本和创建时间匹配，range 拒绝。七种有效但不匹配的 Object metadata 均被拒，且零 Read/Close，保留 caller 所有权。40 轮反向结束顺序：Read 已结束、实际 Close 仍阻塞时 Joined=false；复制句柄共享门禁，排队 Read 不再进入 body，实际 Close 只一次且错误保留。原测试补证 Close 先结束但 Read 未结束，以及 Close panic 不能伪成功。 |

## 限制与交付

没有未通过的产品检查；初次环境 setup 失败已按原记录保留并恢复。未执行仓库全量测试、应用二进制启动、真实 PG/DDL/MinIO、网络产品路径、权限撤销、事务 Unknown、物理恢复或生命周期集成，因为它们不属于此纯块且本次未授权相关资源。

`OwnerReader` 仍只有正式端口；`RevisionMetadata`/`ObjectReader` 构造不是授权证明。`PackageReader.Joined` 仅表示本地同步 Read/Close 已结束，不证明 D05 lease 持久释放。manifest/包解析通过不等于安装成功，也不生成 Skill publication 或 Project 初始化成功事实。

D10 的 Skill 服务、初始化权限闭集、D05 精确 Object Audit facts、D08 四口与 participant 组合、真实发布/清理、PG/MinIO 恢复、Agent/Tool/Runner 和 UI 均仍未在本次验收。这些后段继续保持未绑定/未验证，不能以本报告解除其门槛。

交付时所有本任务测试/编译/vet 命令均已 exit，针对任务路径的运行工具/测试进程检查为空；未创建服务器、容器或数据库资源。临时证据和缓存保留为磁盘材料；精简日志、固定输入、探针与重跑方法已经持久归档。本文及证据目录完成后停止写入，由主线程验收提交，旧状态文档由其另行同步。
