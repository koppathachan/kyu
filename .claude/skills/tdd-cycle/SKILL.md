---
name: tdd-cycle
description: Kent Beck's red-green-refactor and Tidy First discipline for this project. Use when writing any test or any production code in crates/kyu-memory, when unsure whether a change is structural or behavioural, or when deciding what to commit.
---

# The cycle

**Red.** Write one failing test. Run it. Look at the failure and check it fails for the
reason you intended — a test that passes on first run, or fails on a compile error you did
not predict, is not yet a test.

**Green.** Write the minimum code that makes it pass. Minimum is literal. Returning a
hard-coded value is a legitimate green if the test does not distinguish it; the next test
is what forces generality. Do not write the loop before a test needs the loop.

**Refactor.** Only with tests green. One named refactoring at a time. Run the tests after
each one.

# Structural vs behavioural

Every change is exactly one of these, and they never share a commit.

| | Structural | Behavioural |
|---|---|---|
| Changes what the code *does* | no | yes |
| Examples | rename, extract function, move code, reorder fields | new branch, new return value, new state transition |
| Test expectations change | never | usually |
| Commit prefix | `structural:` | `behavioral:` |

When you need both, do the structural change first, on green, and commit it before you
start the behavioural one. If you are mid-behavioural-change and spot a rename you want:
finish the behaviour, commit, *then* rename.

The test for whether a change is really structural: the test suite passes before and after
without a single expectation being edited.

# Minimum means minimum

The pressure in augmented coding is always toward writing more than was asked. Resist it
concretely:

- The test asks for one queue → do not add a queue registry.
- The test asks for ordering by priority → do not add the tiebreak until a test ties.
- You notice the next requirement will need a lock → it can wait for that requirement.

Functionality nobody asked for is a defect, not a head start. It has no test, it constrains
the design, and it is the single most common way these sessions go wrong.

# Never

- Never delete, `#[ignore]`, comment out, or loosen an assertion to reach green.
- Never edit `crates/kyu-conformance/` or `crates/kyu-memory/tests/conformance.rs`.
- Never commit with failing tests or clippy warnings.
- Never do two plan.md items in one cycle.

If a test cannot be made to pass, that is information. Stop and report it.
