# Outbound UI 规格最小证据

本目录只保存规格与静态原件。结论见[报告](../system-outbound-policy-ui-spec-verification.md)；[index.json](index.json)记录全部原路径、SHA256、字节数和固定Git来源。

21逻辑原件，19内容对象（190454字节），两个Git原件复用；57项固定Git引用去重为57个blob。正式卡按 `f843506`，产品来源按 `213cf5c`；旧SMTP原报告按 `2cefd225` 复用。四行政文件原文另按 `f843506` 核保留边界，不称运行依赖闭包。

| 逻辑原件 | 存储 | 字节 |
| --- | --- | ---: |
| `draft-rev1` | [7fa69924faca…](objects/7fa69924facaceaa9944364a68fdf53cdb26655eddf2bf89b48c94e19feb1a71) | 19273 |
| `rev2-card` | [a6b698b72f4e…](objects/a6b698b72f4e213c8decb360c171e3c27444f39e9df0540aa431d56597080d2a) | 28973 |
| `rev2-basis-md` | [6b9c09c71fa4…](objects/6b9c09c71fa4baf340a75b2227ea2b7b64d04282d47122f612a975f8cdad4673) | 2773 |
| `rev2-basis-json` | [5f5911aecda2…](objects/5f5911aecda2ed756fb9cf0b3bb2cd1fc06be8e3dd2fde2f7c00bf86ded06ba0) | 14545 |
| `rev1-to-rev2-diff` | [85681f0c2024…](objects/85681f0c2024b9a2704ff0d7a066eef21e3c3997630c1127a80bf6c50761baaf) | 35010 |
| `rev3-card` | [efc6c8cb8c87…](objects/efc6c8cb8c87c058c5e3f7db0d68bd3c33d6eef13a42fdcbc56fe0196d0699b0) | 32070 |
| `rev3-basis-md` | [90a8b456d0a2…](objects/90a8b456d0a2abfd76f42152d7b81c26def63101974b3ceb89bdac318628924d) | 2078 |
| `rev3-basis-json` | [06bbc1daac1b…](objects/06bbc1daac1bc581ce0de0e58e061d6a8fc5dc7743e9c0b368aa1d28a5aa1717) | 2125 |
| `rev2-to-rev3-diff` | [a4b7a7a8b5ba…](objects/a4b7a7a8b5ba9e257ec7d2c6bdb9fc6f6e884364ab3bb25fc766ec30a9f0e5b4) | 16665 |
| `formal-header-diff` | [cd541cb0348c…](objects/cd541cb0348ce7ed8139554a15f4d3d9f011a9cf5358489842218b4b819fa6be) | 4185 |
| `formal-header-check` | [607b2f6ee3f3…](objects/607b2f6ee3f39967f5e429a9d49d599577fd5dc8191667b08af8e3f5b3e3db8b) | 1127 |
| `rev2-review-md` | [629917c04eea…](objects/629917c04eea41426d53a5edd964625d322430f811c65cbad4d2fcfd38fcfc5d) | 6935 |
| `rev2-review-json` | [7315a9ee6395…](objects/7315a9ee63955dfbaa327c0e15fc7ec2975d12a9f481de665e0108f12fc9d555) | 2681 |
| `rev2-static-check` | [77c5a3b6a085…](objects/77c5a3b6a0857e13cd4207dc85abb006ede6cbe49e1ad3b31f16d676c3aca160) | 10281 |
| `preparation-error` | [6cdc103a8e24…](objects/6cdc103a8e24fb38850f1367ca999f34699886036bf45260dda376f4ecdb69ba) | 598 |
| `rev3-review-md` | [525832e8a7a0…](objects/525832e8a7a08ecddf1b09a0ae641e19003c1aa1fe69c60f54886c00a95ae011) | 2484 |
| `rev3-review-json` | [1bdd46db4f68…](objects/1bdd46db4f684b3d835eb13b2e28db3f0bca69f32353f8a4eacbf07d95c2bece) | 1840 |
| `independent-plan-md` | [ef6233fc7725…](objects/ef6233fc772596821aab14815a32c1b9b7a61b461fd4fc22cf6c8c1a80940f26) | 5857 |
| `independent-plan-json` | [1175f171311b…](objects/1175f171311baf29aaafc908aba889554c725c4dc328b2494b4fec1c5500221d) | 954 |
| `formal-card` | 固定Git `f843506d9ec991be1c334a81b88d5cbacc277467`，路径见index | 32026 |
| `accepted-smtp-review` | 固定Git `2cefd225bf179d166d4fa8b912262712226dd571`，路径见index | 4894 |

```bash
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-outbound-policy-ui-spec-verification-evidence/verify_archive.py
```

私有payload可加 `--repo /workspace/agenteam --documents <payload根>`。校验器只读SHA／Git／差量／范围／链接，不执行原脚本或产品；本目录无实现、浏览器、网络资源通过结论。原no-index退出1和traceback保持，未另存的历史完整Python命令／环境／时长不补造。修后的首次拒绝规则只获规格STATIC PASS；计划仅准备，未来实施和真实A/B另验。
