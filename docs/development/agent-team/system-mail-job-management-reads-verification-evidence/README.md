# 邮件任务管理读口不可变证据

本目录配套[正式接受报告](../system-mail-job-management-reads-verification.md)，绑定产品 `819aba1b8f764328f1e2e67b53c274fad0db877d` 的八源与实际私有基线 `3c79fd4069837c4cee0b1ed37e00480ea8f1909f`。

[index.json](index.json) 的 `artifacts` 将529个逻辑原件映射到SHA256、原字节数和origin；`objects` 仅保存237个唯一对象。原报告内的相对路径未改写，按逻辑名在索引中取件。没有复制完整935基线、3501运行时实体、依赖、二进制或dist。

- `author/`：原阶段报告与旧回归增补、八路径清单／完整diff、api阶段和input01–04、22项离线检查、五轮真实原件、driver版本。三个输入delta的测试前提修订与实际首红保持。
- `independent/`：最终报告／submission、两版pure和三版准备overlay、编译首红、两轮真实原command/input/driver/raw/cleanup与DB log归因。每个失败轮保持原exit及到达边界。
- `static/`：input01条件阻断、input02、rev1.2/input03、input04及driver静审原件，不将静态结论冒作动态执行。
- `delivery`／`manifests`：固定Git八路径与各版精确候选；`runs`：38原检查，其中七真实轮均有actual wait、exact-ID双absent及所属PID/starttime终局。历史PPID1 Z不计回收。

作者阶段MD SHA256 `2af3123baed4dd8dfc9c3a06a4abfaa8a9bcf5f41503d59c6dae1dedf3792fe2`；回归增补 `f57e2281f458b3107139e9ccfff500b61647a7ef3b593c450f409150ac19f746`；独立最终MD `79c928efc63c5f033e2396540fe7c1486f23e62b71682fae83964e0229e552e2`、JSON `d10e30ca09bc7377d33a9a3911e1cc10ea1a3c99daafb297f8e9e3e74280f76d`；最终manifest `4e3bee7144106e9b183977a8306314f08732a18ef54111997fbfb9a3a7ada5c4`。

在仓库根运行纯离线检查：

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-mail-job-management-reads-verification-evidence/verify_archive.py
```

[verify_archive.py](verify_archive.py)校原字节、固定Git、旧OpenAPI和原metadata中的退出／输入／双清；不执行归档脚本或产品，不依赖原scratch。运行时文件由原清单与前后门禁核对应，不声称当前宿主仍具备相同工具或依赖。三行EXPLAIN不是规模SLA，受控HTTP本地3s与PG原1s锁等待分列，A是facade真实调用，B未测试外部邮箱；GET不作receipt也不授retry。
