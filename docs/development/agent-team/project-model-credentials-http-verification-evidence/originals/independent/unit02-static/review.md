# Project credential HTTP #1–17: bounded STATIC PASS

Frozen unit02 `e185ad5f10893cf55176868932281ecf36e7600e5e5715dc797a36fc4ddf60b4` retains all six production02 sources and the four lookup01 sources. This combines their accepted independent static reviews with the unit01 schema/test/root review and the unit02 #7 delta; it is not runtime or complete credential product acceptance.

CRD-UNIT-02 is closed in source: the callback test now owns a separate done channel, releases/cancels in unconditional Cleanup and receives done even after a fatal assertion or consumed result. The added tracked Problem.code/committed-response cases use the real HTTP middleware. Only #7 changes; all other sixteen hashes match unit01. The original finding stays preserved.

The schema closes all request/result variants and resolves only its local schemas plus the accepted common schema. Strict request shape, UTF-8 byte bounds, 400 KiB raw input, metadata/mutation/observation safety and HEAD no-body are consistent with the reviewed production projection. Maximal encoding and material destruction tests inspect actual bytes but do not claim Go string erasure. App controlled construction/route tests do not prove actual same-Store Audit facts; this remains a real integration obligation.

The three native sources remain opt-in and own listener/server/client shutdown. Their 38-second client bound accommodates the separate natural 30-second/2-second body test. Their source presence is not native evidence. Material-safe syscall-only tracing, fresh resource baselines and the native/PG execution window remain unapproved here. No active integration source was read for this 17-source conclusion.

Independent controlled behavior is not run yet. Its private overlay will select only TestIndependentProjectCredentialHTTPControls, with generated TestMain review and ordinary/race preparation separate from execution. The author natural-budget top (~32 s) is excluded from this short independent selector.
