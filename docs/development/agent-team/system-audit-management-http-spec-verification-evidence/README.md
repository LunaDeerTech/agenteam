# System Audit HTTP 规格最小证据

[正式报告](../system-audit-management-http-spec-verification.md)记录有界STATIC结论与授权状态；[index.json](index.json)是原件路径／SHA／字节和固定Git身份的精确索引。

12逻辑原件＝11个SHA对象（105095字节）＋正式卡Git复用。独审47静态来源与作者5个额外链接目标共52项；作者26源码依据已包含在47中。不是运行闭包，不复制源码树、四个重复分段输入索引、独审同SHA私稿副本、依赖或缓存。

| 原件 | 存储 | 字节 |
| --- | --- | ---: |
| `author-basis` | [0cc07d726ff9…](objects/0cc07d726ff94eb76c6abeb07bb57ddad74eccb04e74951b22d0ba4a64f3ece8) | 13024 |
| `author-diff` | [58d3de68005c…](objects/58d3de68005c6b82ef1fa4bf2d63f254f6cfc57ce3bb64087cb728609455f69c) | 32420 |
| `author-draft` | [91d576400771…](objects/91d576400771047c9abda0c5d4fd983d331bfd8c1ff86096701c8edd641756b0) | 32161 |
| `author-freeze` | [b500a3855a99…](objects/b500a3855a996e3060003881ab075a3547d6cab919f67076294dfa66b56722af) | 790 |
| `formal-checks` | [58c854eadf46…](objects/58c854eadf46ad43606d071c89ba613aa603b2b84b48418b14836c510274d5f7) | 1104 |
| `formal-card` | 固定Git `24da141652bbc4c183cd827e767dbdaf2f2f4051` 原卡 | 33147 |
| `formal-freeze` | [d6ec66343329…](objects/d6ec66343329ff0f8ae2df20c023e65033519e213c19f299ef8addba4612853f) | 908 |
| `formal-header_diff` | [b4d181472c85…](objects/b4d181472c854b1f9f8220e31163c47d2ec1784e68e1aa3b41c83b91627bdf89) | 2128 |
| `independent-review-inputs.json` | [50120c4700e4…](objects/50120c4700e4d09f8985e5a38b59a1443868b027993cbeb629460c786862035d) | 10310 |
| `independent-review.json` | [17ad628661a4…](objects/17ad628661a4a4fe7e9d9fb0572b7fca91ae8ca2a54118591441146bb08dde22) | 2325 |
| `independent-review.md` | [ed4cc13288cd…](objects/ed4cc13288cdb39218e68a53a1b92ac0b622e813d81e4534673136ee42befb79) | 7348 |
| `independent-static-arithmetic.json` | [284b8a06d7ad…](objects/284b8a06d7ad95d77f216fd8d4aafe27e6d5568fd6177400c485df95b6525b00) | 2577 |

```bash
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-audit-management-http-spec-verification-evidence/verify_archive.py
```

私有包可加 `--repo /workspace/agenteam --documents <payload根>`。只读固定Git和原字节，核技术不变、14范围、2差量、算术、链接及行政追加，不运行原脚本或产品。两次辅助静态前提失败仅有原review叙述，无单独command/raw/退出时长，不后造证据。原对象空白原样保留；无动态通过或资源授权声明。
