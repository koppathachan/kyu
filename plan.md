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
- [ ] leasing returns a saved message with its payload
- [ ] a leased message carries the attributes it was saved with
- [ ] a leased message carries the priority it was saved with
- [ ] a leased message carries the queue id it was saved to
- [ ] a leased message reports `delivery_count` 1 on its first delivery
- [ ] a leased message carries a non-empty lease token
- [ ] a leased message's lease expires at now plus the requested lease ttl
- [ ] leasing without a lease ttl uses the queue's `default_lease_ttl`
- [ ] a message with a live lease is not returned to a second leaser
- [ ] leasing returns no more messages than the requested batch size
- [ ] acking a leased message reports OK
- [ ] acking a leased message removes it from the queue
- [ ] queue stats count a saved message as ready
- [ ] queue stats count a leased message as leased

<!-- §8.1.2 — Deduplication -->

- [ ] saving with an idempotency key reports `deduplicated` false
- [ ] saving again with the same key inside the dedup window returns the original message id
- [ ] saving again with the same key inside the dedup window reports `deduplicated` true
- [ ] saving again with the same key inside the dedup window stores no second message
- [ ] saving with the same key after the dedup window has passed returns a new message id
- [ ] a duplicate save inside the window returns the original id even after that message was acked
- [ ] saving with an empty idempotency key never deduplicates
- [ ] the same idempotency key in two queues produces two messages

<!-- §8.1.10 — Visibility delay -->

- [ ] a message saved with a visibility delay is not leasable before the delay elapses
- [ ] a message saved with a visibility delay is leasable once the delay elapses

<!-- §8.1.7 — Priority and ordering (§3.2) -->

- [ ] message ids are lexicographically ordered by creation time
- [ ] among visible messages the highest priority is delivered first
- [ ] among equal priorities the message that became visible earliest is delivered first
- [ ] among equal priorities and equal visibility times the lowest message id is delivered first

<!-- §8.1.3 — Lease expiry -->

- [ ] a message whose lease has expired is leasable again
- [ ] redelivery after lease expiry reports `delivery_count` 2
- [ ] redelivery after lease expiry issues a different lease token

<!-- §8.1.4 — Stale token -->

- [ ] renewing with the current token extends the lease to now plus the extension
- [ ] renewing does not rotate the lease token
- [ ] renewing with a token from a previous delivery fails `LEASE_LOST`
- [ ] renewing a lease that has already expired fails `LEASE_LOST`
- [ ] a renew rejected as `LEASE_LOST` leaves the message unchanged
- [ ] settling with a token from a previous delivery reports `LEASE_LOST`
- [ ] a settlement rejected as `LEASE_LOST` leaves the message unchanged

<!-- §8.1.5 — Retry delay -->

- [ ] nacking with a retry delay makes the message unleasable for that duration
- [ ] nacking with a retry delay makes the message leasable once the delay elapses
- [ ] nacking with a zero retry delay makes the message leasable immediately

<!-- §8.1.6 — Dead-lettering -->

- [ ] a message reaching `max_deliveries` by lease expiry moves to `DEAD_LETTER`
- [ ] a message reaching `max_deliveries` by nack moves to `DEAD_LETTER`
- [ ] a dead-lettered message is never delivered again
- [ ] queue stats count a dead-lettered message in `dead_letter_count`
- [ ] with `dead_letter_enabled` false a message reaching `max_deliveries` is deleted instead
- [ ] a forced dead-letter settlement moves a message below `max_deliveries` to `DEAD_LETTER`
- [ ] with `dead_letter_enabled` false a forced dead-letter deletes the message

<!-- §8.1.9 — Batch settle -->

- [ ] settling returns one result per settlement, in request order
- [ ] a batch mixing a valid and a stale settlement applies the valid one
- [ ] a batch mixing a valid and a stale settlement leaves the stale item's message unchanged
- [ ] settling a message that does not exist reports `MESSAGE_NOT_FOUND`
- [ ] a batch containing item-level failures still returns OK for the call itself

<!-- §8.1.11 — Queue isolation -->

- [ ] leasing from one queue never returns another queue's messages
- [ ] deleting a queue reports how many messages it purged
- [ ] deleting a queue removes its messages
- [ ] deleting a queue leaves another queue's messages intact

<!-- §8.1.12 — Reaping -->

- [ ] an expired lease is counted as ready again without any call to `reap_expired_leases`
- [ ] `reap_expired_leases` reports how many leases it returned to ready
- [ ] `reap_expired_leases` reports how many messages it moved to dead letter
- [ ] `reap_expired_leases` with a grace period leaves more recently expired leases alone

<!-- §4 — Limits and validation (normative MUST; enforced on the SPI surface) -->

- [ ] ensuring a queue with an empty queue id fails `VALIDATION_FAILED`
- [ ] ensuring a queue with a queue id over 256 bytes fails `VALIDATION_FAILED`
- [ ] ensuring a queue with `max_deliveries` below 1 fails `VALIDATION_FAILED`
- [ ] saving an empty payload is accepted
- [ ] saving a payload over 1 MiB fails `VALIDATION_FAILED`
- [ ] saving more than 16 attributes fails `VALIDATION_FAILED`
- [ ] saving attributes over 4 KiB serialised fails `VALIDATION_FAILED`
- [ ] saving an idempotency key over 256 bytes fails `VALIDATION_FAILED`
- [ ] leasing with a batch size of zero returns at most one message
- [ ] leasing with a batch size over 100 fails `VALIDATION_FAILED`
- [ ] leasing with a lease ttl under 1 second fails `VALIDATION_FAILED`
- [ ] leasing with a lease ttl over 12 hours fails `VALIDATION_FAILED`
- [ ] renewing with an extension under 1 second fails `VALIDATION_FAILED`
- [ ] renewing with an extension over 12 hours fails `VALIDATION_FAILED`
- [ ] settling an empty list of settlements fails `VALIDATION_FAILED`
- [ ] settling more than 1000 settlements fails `VALIDATION_FAILED`
- [ ] nacking with a retry delay over 12 hours fails `VALIDATION_FAILED`

<!-- §8.1.8 — Concurrency -->

- [ ] concurrent leasers never receive the same message while its lease is live
- [ ] every message saved is delivered at least once under concurrent leasers
- [ ] the number of messages acked under concurrent leasers equals the number saved
