# Fixed OpenAI SDK source recovery

Official repository: https://github.com/openai/openai-python
Exact commit: `becc1d20eed83c1b8d85e15dc131a372d9dc7813`; `_version.py:2` confirms 3.24.0.

Recovered 17 selected source files under `source/`; `source-manifest.json` records the exact official blob/raw URLs, commit, Git blob IDs, byte counts and SHA256. `field-manifest.json` records adopted field declarations and bounded behavior locators, the original D09 research SHA/lines, candidate card SHA and source differences. These two files are evidence, not conformance-test results.

Local reuse check found no matching OpenAI path in /tmp depth3 or /workspace depth5 and no research directory in /tmp depth2; the only archive found by the bounded /workspace depth3 archive search was the existing Go toolchain. No global absence is claimed.

The initial unauthenticated direct HTTPS fetch of the exact `_version.py` failed with DNS `Temporary failure in name resolution`; preserved in `initial-fetch.json`. Direct hostname checks also failed for github.com/api.github.com/raw.githubusercontent.com. The subsequent authorized normal public Git transport succeeded: private bare/object store only, no work repository Git mutations. Exact argv, start/finish times, exits and output paths are in `git-fetch-commands.json`, `git-0.*`, `git-1.*`. Credential helper and extra headers were disabled; no credential files/config were queried or output. The checkout was never installed/imported/executed.

`FETCH_HEAD` and the commit-object hash were verified against the full requested SHA. Every selected source byte sequence was checked against both its recorded SHA256 and the Git blob ID. `commit-object.txt` preserves the public original object. No latest tag/ref was substituted.

Confirmed: `PromptTokensDetails.cache_write_tokens` exists at completion_usage.py:43; there is no need to erase the original D09 mapping.

Candidate gap for independent review: `include_obfuscation` defaults on, and ChatCompletionChunk.obfuscation is an optional native string. The frozen candidate strict envelope omits it while only requesting include_usage. An ordinary default-obfuscated stream therefore needs an explicit bounded treatment before implementation. JSON metadata/moderation and chunk moderation are additional SDK optionals outside the candidate; default live presence is not proven by this static source. This recovery did not change the card or broaden its supported features.

SDK fields do not establish provider account/model availability, network/runtime conformance or successful lifecycle/Usage integration. No SDK/runtime/dependency execution, Provider call, Go/Docker or business-source write occurred.
