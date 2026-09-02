---
description: Generate the test list in plan.md from the spec. Run once, before any code.
---

Read `spec/protocol.md`. Then fill in the **Tests** section of `plan.md`.

Write a list of small, ordered, individually-testable increments that together satisfy
§8.1's thirteen requirements. Each line is one test, phrased as the behaviour it pins down:

```
- [ ] saving a message to a queue returns a message id
- [ ] leasing from an empty queue returns nothing
```

Rules for the list:

- Dependency order first, then smallest-first. The first line must be the smallest thing
  that can fail.
- One behaviour per line. If a line needs the word "and", it is two lines.
- §8.1 is the whole scope. Do not add lines for behaviour the spec does not require.
- Group the lines under a comment naming the §8.1 requirement they serve, so coverage is
  checkable against the spec.

**Write only the list. Do not write any test code, any implementation, or any file other
than `plan.md`.** Stop when the list is written and report how many tests it has.
