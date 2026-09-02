---
description: Run one TDD cycle — the next unmarked test in plan.md.
---

go

Find the next unmarked test in plan.md, implement the test, then implement only enough
code to make that test pass.

Precisely:

1. Read `plan.md`. Take the **first** `- [ ]` line. State which one you are doing.
2. Write that one test. Run the tests. **Show me the failure** — a test that has not been
   seen to fail has not been written.
3. Write the minimum code to make it pass. Not the general case. Not the next test's case.
4. `cargo test` — all green. `cargo clippy --all-targets -- -D warnings` — clean.
5. Commit as `behavioral: <what the test pins down>`.
6. Now consider structure. If there is duplication or unclear naming, tidy it — one
   refactoring at a time, tests run after each — and commit separately as
   `structural: <the refactoring's name>`.
7. Mark the line `- [x]` in plan.md.
8. Stop. Report: the test, the failure you saw, the code you added, the commits. Then wait.

Do not do two tests. Do not implement anything the test did not demand. Do not touch
`crates/kyu-conformance/` or `tests/conformance.rs`.

If you cannot make the test pass, say so and stop. Do not weaken the test, skip it, or
move on to a different one.
