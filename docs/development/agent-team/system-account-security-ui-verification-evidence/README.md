# Account security 不可变证据

本目录配套[正式接受报告](../system-account-security-ui-verification.md)，绑定产品 `40c904c0dd88420fc621f40c0a737d243d514ec1`、input06 的 26 源／33 dist 指纹及最后 README；卡技术§1–7保持原字节。

[index.json](index.json) 的 `artifacts` 将 559 个逻辑原件映射到 SHA256、字节数及原 origin，`objects` 给出实际相对位置。353 个对象保存在本目录，35 个同 SHA 对象直接复用已提交 Dialog fallback／Selection 取消读取档案。原始文件的相对引用未被改写，可按逻辑路径继续在索引中查找；SHA 命名文件保留原字节和原格式。

- `delivery`：固定 Git 27 路径及对应对象；`source_manifests`：六版 26 源、dist 指纹及各接受基线；`fixed_early_git_references`：早期不变 Git 来源。最终基线 1051 文件，不读取活动工作树作产品结论。
- `runs`：64 原检查，包括十轮真实 command/raw/result/cleanup 与 actual wait；保留产品红、诊断、私有前提错误和分版本复用，失败轮仍按原 exit 记录。
- `permanent_dependencies`：两个既有档案的固定 index SHA 与归档提交；不复制完整基线、依赖、dist 实体、缓存或二进制。
- `bounded_missingness`：历史 Go 编译未消费的 browser 前态 `72e3a507…40d37` 缺精确副本，原指纹／命令／退出仍在，不补造。全部最终受测源与失败源可定位。
- `limits`：input02 未执行、旧现场不能追填、A/B 使用 input04 的差量复用、作者图审归属及历史 PPID1 Z 等原限制。

最终作者报告 SHA256 `0941ae0e13fd556d220e719b0fb6da3159630aef8a57eeefb613d529b0d1b7b0`；独立本域报告 `6120ebb1032d2f2a6ebbeb6696aa2285f2cbf87fbc537f9faf9d3b8f1e141680`；独立组合增补 `3edbe3a75dfb85defce11446988b653e0f04813f401fd0768cb104193915882a`；最终 27 路径 manifest `c0a497bbf9d9b1c86d3f0f64f2355c00060948b0d22191a3dbbbda9e8afb24c4`。

在仓库根只运行离线检查：

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-account-security-ui-verification-evidence/verify_archive.py
```

[verify_archive.py](verify_archive.py)仅校验保存字节、固定 Git、原 metadata 中的退出／实际 wait／双清和范围，不执行归档脚本、产品测试或资源动作，不依赖原 scratch。离线 PASS 不把旧失败改成通过，也不补齐声明缺失的历史源码或完整环境。
