# D26 独立探针 READY v2

**READY，仍未运行浏览器或 Docker。** 相对 v1 只把独立 spec 中 firstQuestion/nextQuestion 两处创建挑战响应断言从 200 对齐为正式 201。固定 `457b1979...:internal/central/account/http_auth.go:159` 使用 http.StatusCreated；这是 root 运行前发现的探针笔误，不是产品失败或动态失败后的断言放宽。

原 spec `dadd6fbb…`、ready/input/plan/index 的原字节保存于 evidence/ready-v1；原 ready.md 亦未改变。两行精确差量见 evidence/ready-v1-to-v2.patch。CHALLENGE_REQUIRED=401、CHALLENGE_INVALID=400、匿名 CSRF_FAILED=403、成功 verify/login=200 与 logout=204 均保留；其余链、同 key/输入、实际焦点、Cookie/DB/Audit、材料保护及预算不变。

本次仅重做 TypeScript，typescript-02 exit=0（0.915s）；Go/config 字节未变，复用此前 race compile/vet/config syntax 的实际通过。567 输入末核匹配，无新增安装或资源运行。

最终 selector：`^TestAccountAuthenticationWebIndependent$`。正式运行必须使用 `python3 /workspace/agenteam-d26-independent-v-2wzub6ar/observe.py /workspace/agenteam-d26-independent-v-2wzub6ar/plan-v2.json 0`，并另获 root 窗口。

- spec SHA-256 `f2dc0cea424d056023fab86d9c58f46de03a3ea2e7ba70e57212acbee4b5be35`
- input-v2.json `2c110cf32890f54d300c8535113c11a641dcf05908942e0d91cb6f942026417e`
- plan-v2.json `d82897b4cebb403134a29a62f1f74e0e927a6b9075ffa89b58bf8c9b52f7bf52`
- observer 未变 `323da433bf56caaec5d01b5b701266a93d9f1854b04e8ce2322bdaafaf1d7fca`
