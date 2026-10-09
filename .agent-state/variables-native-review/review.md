# Variables Authority diagnostic review

Review base: Variables tree `0ab5b239`, targeted repair `8556803a`.
These assets run only offline, with Runner-owned cache/output and a read-only Go
source overlay. They start no server, OS socket, PostgreSQL or browser, and do
not write the reviewed tree.

Run from the Runner tree:

```sh
python3 .agent-state/variables-native-review/boundary.py
```

`boundary.py` invokes the actual Account HTTPBoundary and request-ID middleware
with an HTTP Recorder. Original Problem bytes and XID enter the actual extracted
Go refusal method. Explicit SQL-result substitutes only test its binding/control
flow; they cannot prove PostgreSQL facts. The produced Problem also passes the
formal schema and enters the actual Playwright-transformed/serialized installer.
The latter uses explicit Session/native/jsdom substitutes and the actual dist
public-symbol selector; it does not represent a real browser or owner join.

`retirement-deadline.cjs` uses the actual transformed Node sampler. A controlled
Date.now crosses the original 250 ms deadline while its evaluate Promise settles
before timer dispatch. It requires false, without extending or replacing the
sampler's deadline. This is a deterministic scheduling boundary, not an actual
browser timing result.

Actual review facts:

- Original 36 observer controls: `5399 → ae51bf`, exit 0, own PW cache.
- Actual producer into the original Go method: `84124 → 9577c4`, exit 1;
  `/api/v1`, one diagnostic failure, zero SQL calls. Initial `e27989` was an
  internal-import setup failure, corrected with a read-only overlay.
- Original serialized observer with actual producer instance:
  `31437 → a2ca92`, exit 1, `instance_matches=false`.
- Original sampler at elapsed 251 ms: `46480 → b76fa3`, exit 1;
  it incorrectly returned retirement true.
- Final repaired sources: `24158 → 8cb191`, actual exit 0. Actual producer and
  schema pass; actual Go method has zero failures and one SQL call; serialized
  observer binds actual instance/XID/native identity and retires its hook;
  the original 251 ms counterexample returns false.

The three assets were present after environment recovery; their prior partial
output is not a replacement for a missing terminal. The final result above was
personally rerun and polled to actual completion after recovery.

No remaining must-fix was found in this limited diagnostic review. Exact
endpoint/method/query/private request/key/CSRF binding remains separate from the
public Problem.instance. Old failed/finished/schema/client/SQL acceptance gates,
resource budgets and production/dist remain unchanged. The original Authority02
whole failure is not reclassified. New binary08 and real authority behavior
remain unverified by this review.

During persistence, a literal-formatting edit produced a Python parse failure
(`f57262`). It was corrected without rerunning or changing product sources;
`7b14ca` verifies parsing, both Node syntax checks, diff whitespace, and that the
final embedded Go plus extracted method is byte-identical to the program that
actually passed above. This formatting failure is separate from product probes.
