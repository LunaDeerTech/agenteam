# 系统邀请最近投递读口最小证据包

接受范围见[正式报告](../system-invitation-delivery-read-verification.md)。本包保存已冻结原件，未运行产品、浏览器、数据库或归档中的 driver。

- [archive-index.json](archive-index.json)将168项逻辑原件映射到101份去重文件，共1,578,315字节；记录原绝对路径、SHA-256、字节数和用途。同字节只保存一份，逻辑路径不保证另有物理副本。
- 作者四版输入含精确八源和两个Go锁；其余源码从产品基线 `7ef3e30cf06b5df6d19516f24c34855a8308ef05` 重建。最终八源绑定正式交付 `9b3201547f9b7b61fd9716a6ba6540084961496c`。源码和历史可执行脚本以文本原件保存，不复制完整仓库、模块/编译缓存、binary、runtime。
- 作者原三红、七组组合复用、15条检查、五轮实际命令/raw/输入/双清原件保留；独立两probe/overlay、compile01与independent01原命令、两报告及静审记录保留。原scratch路径仅为历史标识，重建时须改用自有路径，不能直接运行其中资源命令。
- plan01和author01共21份完整EXPLAIN JSON已经包含在原raw，不重复复制派生计划；两份摘要只是导航，脚本重新解析原raw核时间。plan01两份旧测试源原件缺失，不宣称可完整重建；最终迁移/规模结论绑定author01原源码和raw。pure03缺env原样保留，不拼装假原命令。

离线检查只读取本包与固定本地Git对象，不读取当前活动产品树、不联网、不执行原测试：

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-invitation-delivery-read-verification-evidence/verify_archive.py
```

预期PASS为168逻辑/101物理原件、四输入、八交付源、两未变锁、18未变迁移与唯一00019、15作者命令/1独立编译、六轮原日志与实际清理、21份原计划和48个固定Git blob。该结果是持久证据完整性检查，不是重跑业务；原失败、缺件、未知与接受边界以正式报告及各原报告为准。归档原件保持冻结原字节，格式检查不改写其历史空白。
