# plan.md

The test list. This file is the work queue.

## How this file works

- Every line under **Tests** is one test: `- [ ]` not yet written, `- [x]` written and passing.
- On **"go"**: take the *first* unmarked test, write it, make it pass with the minimum code,
  refactor if needed, mark it `[x]`, commit.
- One test per "go". Never work ahead.
- If a test turns out to be too big to do in one cycle, split it into smaller lines *in this
  file*, then do the first one. Say that you split it.
- If you think a test is missing, propose it here and wait — do not just implement it.
- Ordering is dependency order, then smallest-first. The first test should be the smallest
  thing that can fail.

## Tests

<!--
  Empty on purpose. Run /plan to generate the list from spec/protocol.md §8.1,
  then it gets reviewed before any code is written.
-->
