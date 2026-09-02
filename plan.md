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
  Proto adapter — generated `kyu.storage.v1` types <-> `spi::*`.
  §1: the in-process and gRPC bindings are the same contract. These conversions
  are what prove the hand-transcribed trait and the compiled proto agree.
-->

- [x] a `Duration` converts to its proto form and back
- [x] a `SystemTime` converts to a proto `Timestamp` and back
- [x] an absent proto `Duration` converts to `None`
- [x] `MessageState` converts to its proto enum and back
<!--
  DEFERRED (option 1): the remaining conversions are written when the gRPC
  server is built, driven by what it actually touches — request types inbound,
  response types outbound, `Error` -> `tonic::Status`, `Outcome`. Writing them
  up front would produce ~29 field-for-field mappings with no caller, none of
  which §8.1 exercises. The four above exist because the adapter needed a
  starting point; `message_state_*` has no caller either and can go if the
  server never needs it.
-->

<!-- §8.1.13 — Provider info -->

- [x] `get_provider_info` reports `contract_version` "kyu.storage.v1"

<!-- Queue registry — prerequisite for §8.1.1 and §8.1.11; error codes per §7 -->

- [x] ensuring a queue that does not exist reports it was created
- [x] ensuring a queue that already exists reports it was not created
- [x] ensuring an existing queue returns the stored config, not the requested one
- [x] getting a queue's config returns the config it was ensured with
- [x] getting the config of an unknown queue fails `QUEUE_NOT_FOUND`
- [x] listing queues returns the queues that have been ensured
- [x] listing queues returns no more queues than the requested page size
- [x] listing queues continues from the page token of the previous page
- [x] saving to an unknown queue fails `QUEUE_NOT_FOUND`
- [x] leasing from an unknown queue fails `QUEUE_NOT_FOUND`

<!-- §8.1.1 — Lifecycle: save → lease → ack -->

- [x] saving a message to a queue returns a message id
- [x] leasing from a queue with no messages returns no messages
- [x] leasing returns a saved message with its payload
- [x] a leased message carries the attributes it was saved with
- [x] a leased message carries the priority it was saved with
- [x] a leased message carries the queue id it was saved to
- [x] a leased message reports `delivery_count` 1 on its first delivery
- [x] a leased message carries a non-empty lease token
- [x] a leased message's lease expires at now plus the requested lease ttl
- [x] leasing without a lease ttl uses the queue's `default_lease_ttl`
- [x] a message with a live lease is not returned to a second leaser
- [x] leasing returns no more messages than the requested batch size
- [x] acking a leased message reports OK
- [x] acking a leased message removes it from the queue
- [x] queue stats count a saved message as ready
- [x] queue stats count a leased message as leased

<!-- §8.1.2 — Deduplication -->

- [x] saving with an idempotency key reports `deduplicated` false
- [x] saving again with the same key inside the dedup window returns the original message id
- [x] saving again with the same key inside the dedup window reports `deduplicated` true
- [x] saving again with the same key inside the dedup window stores no second message
- [x] saving with the same key after the dedup window has passed returns a new message id
- [x] a duplicate save inside the window returns the original id even after that message was acked
- [x] saving with an empty idempotency key never deduplicates
- [x] the same idempotency key in two queues produces two messages

<!-- §8.1.10 — Visibility delay -->

- [x] a message saved with a visibility delay is not leasable before the delay elapses, then is

<!-- §8.1.7 — Priority and ordering (§3.2) -->

- [x] message ids are lexicographically ordered by creation time
- [x] among visible messages the highest priority is delivered first
- [x] ties are broken by earliest `visible_at`, then by lowest message id

<!-- §8.1.3 — Lease expiry -->

- [x] a message whose lease has expired is leasable again
- [x] redelivery after expiry reports `delivery_count` 2 and a different lease token

<!-- §8.1.4 — Stale token -->

- [x] renewing with the current token extends the lease to now plus the extension, token unchanged
- [x] renewing with a rotated or expired token fails `LEASE_LOST` and changes nothing
- [x] settling with a rotated token reports `LEASE_LOST` and changes nothing

<!-- §8.1.5 — Retry delay -->

- [x] nacking with a retry delay makes the message unleasable for that duration, then leasable
- [x] nacking with a zero retry delay makes the message leasable immediately

<!-- §8.1.6 — Dead-lettering -->

- [x] a message reaching `max_deliveries` by lease expiry moves to `DEAD_LETTER` and is counted there
- [x] a dead-lettered message is never delivered again
- [x] a message reaching `max_deliveries` by nack moves to `DEAD_LETTER`
- [x] a forced dead-letter settlement moves a message below `max_deliveries` to `DEAD_LETTER`
- [x] with `dead_letter_enabled` false a dead-lettered message is deleted instead

<!-- §8.1.9 — Batch settle -->

- [x] settling returns one result per settlement, in request order
- [x] a batch mixing a valid and a stale settlement applies the valid one and leaves the stale one alone
- [x] settling a message that does not exist reports `MESSAGE_NOT_FOUND`

<!-- §8.1.11 — Queue isolation -->

- [x] leasing from one queue never returns another queue's messages
- [x] deleting a queue removes its messages and reports how many it purged
- [x] deleting a queue leaves another queue's messages intact

<!-- §8.1.12 — Reaping -->

- [x] an expired lease is counted as ready again without any call to `reap_expired_leases`
- [x] `reap_expired_leases` reports how many leases it returned to ready
- [x] `reap_expired_leases` reports how many messages it moved to dead letter
- [x] `reap_expired_leases` with a grace period leaves more recently expired leases alone
- [ ] `reap_expired_leases` deletes rather than dead-letters when `dead_letter_enabled` is false

<!-- §4 — Limits and validation (normative MUST; enforced on the SPI surface) -->

- [ ] an empty or oversized `queue_id` fails `VALIDATION_FAILED`
- [ ] `max_deliveries` below 1 fails `VALIDATION_FAILED`
- [ ] saving an empty payload is accepted
- [ ] a payload over 1 MiB fails `VALIDATION_FAILED`
- [ ] more than 16 attributes, or over 4 KiB serialised, fails `VALIDATION_FAILED`
- [ ] an idempotency key over 256 bytes fails `VALIDATION_FAILED`
- [ ] a batch size of zero leases at most one message
- [ ] a batch size over 100 fails `VALIDATION_FAILED`
- [ ] a lease ttl outside 1 second to 12 hours fails `VALIDATION_FAILED`
- [ ] a renew extension outside 1 second to 12 hours fails `VALIDATION_FAILED`
- [ ] an empty settlement list fails `VALIDATION_FAILED`
- [ ] more than 1000 settlements fails `VALIDATION_FAILED`
- [ ] a retry delay over 12 hours fails `VALIDATION_FAILED`

<!-- §8.1.8 — Concurrency -->

- [ ] concurrent leasers never receive the same message while its lease is live
- [ ] every message saved is delivered at least once under concurrent leasers
- [ ] the number of messages acked under concurrent leasers equals the number saved
