# Model M3 固定 harness 恢复

这里保存原 Model 树 `fbf5876229c4cd53113dd450e2b5af21f0c53c71` 中仍被正式结果引用的测试源码，逐字节保留，不依赖继续保有该 feature ref。`tests/model/` 中七份 M3 测试和两个实际搭配的 helper（`runtime_native_test.go`、`runtime_persistence_test.go`）构成恢复 overlay；其它共用测试、fixture、模块依赖及产品 API 由 main 提供。原结果与失败边界见[Model 恢复卡](../../../docs/development/work-items/m3-model-runtime.md)和[正式独审](../../../docs/development/work-items/m3-core-independent-review.md)。

main 的现有 `tests/model` helper 与原 M3 harness 不同：缺少 `newTextRuntimeFixtureWithTransport` / `newTextRuntimeFixtureWithStore`、`usageOwner` / `retirement`，以及 M3 consumer、response format、embedding input 的 fixture 列和解析/权限支持。因此七份测试没有直接加入 main 的当前测试包，也没有覆盖 main helper。这是已验原源码的可恢复集合，不是新增产品通过结论。

独立的 `tests/model/retentionprobe/constraints_test.go`、`tests/model/probe_embedding_dimension.py` 和 `internal/central/app/model_cold_m3_integration_test.go` 保存在 main 原正式路径。cold App 所依赖的四份 M1/M2 workflow/helper 与原 Model 树逐字节一致。公开向量继续只使用已有的 `../runtime-embeddings-v2/vectors.json.gz`，不恢复 raw 副本或旧 binary。

从 main 仓库根生成临时 overlay 即可恢复编译输入；不复制整个仓库、不替换当前 helper，也不需要旧分支：

```sh
python3 - <<'PYCODE'
from pathlib import Path
import json
root = Path.cwd().resolve()
source = root / '.agent-state/m3-model-runtime/reproduction'
output = root / 'output/ai/m3-model-reproduction'
(output / 'bin').mkdir(parents=True, exist_ok=True)
replace = {str(root / p.relative_to(source)): str(p)
           for p in (source / 'tests/model').glob('*.go')}
(output / 'overlay.json').write_text(json.dumps({'Replace': replace}, indent=2))
PYCODE
GOTOOLCHAIN=local GOMAXPROCS=2 GOFLAGS='-mod=readonly -p=1' \
GOCACHE=/workspace/.cache/go-build GOMODCACHE=/workspace/go/pkg/mod \
/workspace/toolchains/go1.27.1/bin/go test -tags=integration \
  -overlay=output/ai/m3-model-reproduction/overlay.json -c \
  -o output/ai/m3-model-reproduction/bin/ \
  ./tests/model ./tests/model/retentionprobe ./internal/central/app
```

该命令只编译；跨设备使用对应的 Go1.27.1 与缓存位置。原集成测试还需要仓库正式自有 PostgreSQL/MinIO/outbound fixture；live probe/Runtime 需要明确授权的真实端点，保存材料不授权再次请求。没有因本次收拢重跑历史 M3、数据库、外部 HTTP 或测试 binary。

本次恢复检查：2026-10-15 在当前 main 产品/API 上，固定 Go1.27.1、上述九文件 overlay 的唯一一次三包 compile-only 原进程 exit0，生成 model.test、retentionprobe.test、app.test；未执行测试。原 cold App 在当前 helper 下编译成功；另外七份 Model 测试只有恢复 overlay 组合得到这一编译结论，不能描述为 main 默认测试集已经集成或 M3 新 PASS。11份 Go 源通过只读 gofmt 语法/格式检查，Python probe 通过 AST 解析。
