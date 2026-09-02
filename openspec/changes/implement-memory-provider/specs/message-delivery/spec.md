## ADDED Requirements

### Requirement: Delivery order
Among messages eligible for delivery, the provider SHALL select in the order `priority DESC, visible_at ASC, message_id ASC`. A message SHALL be eligible only when its `visible_at` is at or before the provider's current time and it is in the `READY` state.

#### Scenario: Higher priority is delivered first
- **WHEN** messages are saved with priorities 0, 5, -3 and 5 in that order and all four are leased in one call
- **THEN** they are returned in the order: the first priority-5 message, the second priority-5 message, the priority-0 message, then the priority-(-3) message

#### Scenario: Ties fall through to the message id
- **WHEN** two messages share a priority and became visible at the same instant
- **THEN** the one with the lexicographically smaller `message_id` is delivered first

#### Scenario: Invisible messages are not eligible
- **WHEN** the only messages in a queue have a `visible_at` in the future
- **THEN** `LeaseMessages` returns no messages, regardless of their priority

### Requirement: Leasing transitions READY to LEASED
A delivery SHALL move a message from `READY` to `LEASED`, increment its `delivery_count`, issue a fresh `lease_token`, and set `lease_expires_at` to the provider's current time plus the effective lease TTL. The `delivery_count` returned to the caller SHALL include the current delivery.

#### Scenario: A first delivery
- **WHEN** a freshly saved message is leased
- **THEN** the returned message has `delivery_count = 1`, a non-empty `lease_token`, and a `lease_expires_at` in the future

#### Scenario: Statistics follow the transition
- **WHEN** the only message in a queue is leased
- **THEN** `GetQueueStats` reports `ready_count = 0` and `leased_count = 1`

### Requirement: A live lease is exclusive
A message in `LEASED` state whose lease has not expired SHALL NOT be returned to any other caller.

#### Scenario: A second lease attempt sees nothing
- **WHEN** the only message in a queue is under a live lease and `LeaseMessages` is called again
- **THEN** no message is returned

### Requirement: Batch size and lease TTL defaults
`LeaseMessages` SHALL return at most `batch_size` messages, and SHALL treat a `batch_size` of zero as one. When `lease_ttl` is absent the provider SHALL use the queue's `default_lease_ttl`.

#### Scenario: Fewer messages than requested
- **WHEN** `LeaseMessages` requests a larger `batch_size` than the queue has eligible messages
- **THEN** every eligible message is returned and the call succeeds

#### Scenario: An omitted TTL uses the queue default
- **WHEN** `LeaseMessages` omits `lease_ttl`
- **THEN** the returned `lease_expires_at` reflects the queue's `default_lease_ttl`

### Requirement: Leasing is atomic under concurrency
Under concurrent callers, the provider SHALL NOT return the same message to more than one caller while a lease on it is live, SHALL NOT lose messages, and SHALL keep `GetQueueStats` consistent with the leases it has issued at every observable point.

#### Scenario: Thirty-two concurrent leasers drain one queue
- **WHEN** 32 threads repeatedly lease batches from a single queue holding 1000 messages under a lease TTL long enough that nothing expires, and ack every message they receive
- **THEN** exactly 1000 deliveries occur, no `message_id` is delivered twice, every saved message is delivered once, and the queue ends at `(0, 0, 0)`

#### Scenario: Statistics never under-report live leases
- **WHEN** one caller holds a batch of leased messages it has not yet settled and another caller reads `GetQueueStats`
- **THEN** the held messages are counted in `leased_count`
