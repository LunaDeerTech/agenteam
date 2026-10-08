# legacy-profile-json-v2：无关旧排版已恢复，STOP

v1 与其 format-check01 原 FAIL、原件/manifest/SHA 全部不变。

唯一获准的 Prettier --write 实际 exit0、0.618533796 秒。direct PID279358/start1482710 已实际 wait；adopted=[]、owned 两次扫描空、无 timeout。输入唯一变化为目标 spec；作为写格式轮，inputs_same=false 如实保留，不记为输入相同。

formatter 仅把原 helper 的单行 `if (!observed.result.ok) throw new Error("settings JSON observation failed");` 拆为两行，未产生授权 target/callsite 内的格式变化。完整 formatter 输出 SHA dab4013cb76c4514f231df0d7b1334e4670a36893b334ccf3f65e34f708a8081 与相对 preformat/base 的小 delta 已保存。

该变化在原 helper 范围外，按 root 指示恢复无关排版。最终源码逐字节等于 v1 候选：a0fede03ce80118de78a9ba033540bca25587288927791f2daa761c48ea30603；相对原 155b 基线仍只有既定两处差量，复用 v1/delta.patch。

按本次边界，未运行 format-check02 或 TypeScript；无 list、业务/browser、Go、build、suite、资源或外网。没有改其他仓库路径。当前源码格式检查没有新的 PASS，profile 产品/完整 D27 也未获接受；原真实 FAIL 保持。

源与本版本 scratch 已 STOP，等待 root 对这一行既存格式例外的后继决定。
