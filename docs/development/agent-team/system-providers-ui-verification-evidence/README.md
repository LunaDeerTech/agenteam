# Provider UI 验收证据

本目录绑定接受提交 `f465f45b899e21c225e7d4107c099b256538239c`；结论与历史限制见[正式报告](../system-providers-ui-verification.md)。[索引](archive-index.json)将每项逻辑原路径映射到原字节、SHA-256、长度和去重对象，或固定 Git blob。`source` 只是历史定位，不是复核时的读取路径。

676项逻辑原件对应409个物理文件（5,374,101字节），另16项未变源码/锁直接引用 `1c82d88`。保留作者七输入、API/owner阶段原红及关闭、原命令/退出/实际wait、六轮作者和三轮独立真实结果/资源终局、私有probe与driver版本、两版拒收代表图及最终八图。原报告中README pending、候选未验等文字保留其当时事实；最终第22路径由 `author/readme-final.json` 单独绑定。

在仓库根执行：

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-providers-ui-verification-evidence/verify_archive.py
```

[离线脚本](verify_archive.py)只读本目录和本地固定Git：核所有原件、22个交付路径、1026项基线来源（最终1016项只读依赖）、后端/迁移1–19未变、阶段复用、九轮前后输入/actual wait/原终局、new01补清和独立02→03唯一断言差量。它不读取scratch、活动产品树或dist，不执行保存的脚本/测试，不启动资源或访问网络。通过只说明归档自洽和固定来源相符，不是新一轮产品通过。

重建候选使用 `1c82d888adfef0d8b58ab51c920557ca4e2f084f` 加所选 `author/inputNN.json` 的精确源文件；已接受版本的22路径可直接从 `f465f45` 取。独立私有覆盖取各轮 `source/`，不得拿后来top-level probe替代旧轮；`source_relocations`明确旧准备记录的同SHA保存位置。26项dist只保留哈希、原构建命令与前后输入，不承诺另次构建复现同一字节；不复制依赖实体、缓存或二进制。

作者pure03/04缺历史完整源字节，只保留原指纹/命令/失败日志。new01原cleanup=false与后续补清分列；独立最终是02 Replay与03 Partial组合，不是一轮全绿。被拒new03/new04各保留light1440/dark390代表原图，其余原图哈希仍在作者报告；caption-layout诊断是固定CSS/简化DOM，不是正式业务。历史非自有PPID1 Z不在本次清理声明中。原件不重排、不格式化，不将缺失内容重制为原始记录。
