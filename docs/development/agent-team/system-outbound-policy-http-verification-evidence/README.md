# Outbound HTTP 最小不可变证据

[正式报告](../system-outbound-policy-http-verification.md)绑定接受产品 `a94277982620f01dc15488b09ae6ea9064977b5a`、固定基线 `819aba1b8f764328f1e2e67b53c274fad0db877d` 和卡技术 SHA `44515e212a996d1a1413022a38ae2b56297ad7e5cd9d195d732225aab818f8dc`。

[index.json](index.json)按原逻辑路径列 SHA256、字节数和原来源；`object` 指向本目录原字节，`git` 指向固定提交与文件路径。581逻辑原件去重为213对象／5,562,261字节及286个Git引用。`origin`是历史定位，不依赖scratch继续存在；当前源码不参与结论。相同SHA只保存一次，既有Git字节直接复用。

可从 `author/evidence/api03/` 定位原native产品RED；`author/runs/outbound-http-real01/` 与 `author/runs/outbound-http-transaction-real02/` 分别保留原4 PASS+fixture FAIL和单组修复PASS；`independent/runs/compile-account01/` 与probe01/02保留私有类型编译原红；`independent/runs/independent01/` 保存两代表首轮PASS及完整实际终局。原报告页首中的“未接受／待执行”是其生成时的阶段事实，不回写原件。

35条实际检查包括三轮真实资源运行；离线120s检查器与真正资源driver分别保存。固定Git加候选/探针版本可重建已保存的受测源码，421最终依赖与3503／3506运行时清单只保存必要原指纹；不复制依赖/cache/二进制。早期harness-compile01附加闭包SHA73ca6172…95884全文未定位的限制保留，不宣称所有历史闭包均完整。原始日志/diff内的空白保留。

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-outbound-policy-http-verification-evidence/verify_archive.py
```

[verify_archive.py](verify_archive.py)只读对象和固定Git blobs，校验12交付、技术正文、原失败/源版本、35检查退出和三轮精确ID/PID双清。不开网络或业务资源，不执行任何存档脚本，不将原运行时指纹说成当前二进制已复验。历史PPID1 Z非自有，保持未触碰／未回收边界。
