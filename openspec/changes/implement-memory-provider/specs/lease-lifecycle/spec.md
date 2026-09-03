## ADDED Requirements

### Requirement: Every delivery issues a fresh lease token
The provider SHALL issue a new `lease_token` on every delivery of a message. Tokens SHALL NOT be reused across deliveries of the same message.

#### Scenario: Redelivery rotates the token
- **WHEN** a message is leased, its lease expires, and it is leased again
- **THEN** the second delivery carries a `lease_token` different from the first

### Requirement: Renewal extends from now and does not rotate
`RenewLease` SHALL set `lease_expires_at` to the provider's current time plus `extension`, not to the current expiry plus `extension`, and SHALL NOT rotate the `lease_token`.

#### Scenario: A live lease is renewed
- **WHEN** `RenewLease` is called with the current token of a live lease
- **THEN** the call succeeds, the returned `lease_expires_at` is in the future, and the original token continues to settle the message

### Requirement: Stale and unissued tokens are rejected without side effects
`RenewLease` and settlement carrying any token other than the one from the current delivery SHALL fail with `FAILED_PRECONDITION` / `LEASE_LOST` and SHALL leave the message unchanged.

#### Scenario: Renewing with an unissued token
- **WHEN** `RenewLease` is called with a token that was never issued
- **THEN** the call fails with code `FAILED_PRECONDITION` and reason `LEASE_LOST`

#### Scenario: Settling with an unissued token
- **WHEN** a settlement carries a token that was never issued
- **THEN** its per-item result is `FAILED_PRECONDITION` / `LEASE_LOST`

#### Scenario: The message is untouched by a rejected attempt
- **WHEN** a rejected renewal and a rejected settlement have been attempted against a message under a live lease
- **THEN** the message is still `LEASED`, statistics are unchanged, and the real token still renews and settles it

#### Scenario: A token from a previous delivery is dead
- **WHEN** a message has been redelivered and a settlement carries the token from the earlier delivery
- **THEN** its per-item result is `FAILED_PRECONDITION` / `LEASE_LOST` and the current lease is unaffected

### Requirement: Lease expiry is final
A lease that has expired SHALL NOT be renewable, whether or not the message has been redelivered. On expiry without settlement the message SHALL become eligible for delivery again with its `delivery_count` intact, and the expired token SHALL be dead from that moment.

#### Scenario: Renewing after expiry
- **WHEN** a lease expires and `RenewLease` is called with its token before any redelivery
- **THEN** the call fails with code `FAILED_PRECONDITION` and reason `LEASE_LOST`

#### Scenario: Redelivery preserves the delivery count
- **WHEN** a message delivered once has its lease expire and is delivered again
- **THEN** the second delivery reports `delivery_count = 2`

### Requirement: Acknowledgement removes the message
A settlement with outcome `OUTCOME_ACK_DELETE` against a live lease SHALL remove the message permanently.

#### Scenario: An acked message is gone
- **WHEN** a leased message is acked
- **THEN** the queue reports `(0, 0, 0)` and the message is never delivered again

### Requirement: Retry makes the message visible after the given delay
A settlement with outcome `OUTCOME_RETRY` SHALL return the message to `READY` with `visible_at` set to the provider's current time plus the supplied `retry_delay`. A `retry_delay` of zero SHALL make the message immediately eligible. The provider SHALL apply the delay it is given and SHALL NOT compute backoff policy itself.

#### Scenario: A nack with a delay hides the message
- **WHEN** a leased message is nacked with a retry delay and a lease is attempted straight away
- **THEN** no message is returned

#### Scenario: The message returns after the delay
- **WHEN** the retry delay has elapsed
- **THEN** the message is leasable again, no earlier than the full delay after the nack, and its `delivery_count` is incremented on that delivery

### Requirement: Batch settlement never fails as a whole for item-level problems
`SettleMessages` SHALL return `OK` with one `SettlementResult` per input item, in request order, each carrying its own canonical code and reason. Valid items SHALL be applied; invalid items SHALL change nothing. The RPC itself SHALL fail only on request-level validation failure.

#### Scenario: A batch mixing a valid and a stale item
- **WHEN** a batch contains one settlement with a live token and one with a rotated token
- **THEN** two results are returned in request order, the first with code `OK` and the second with `FAILED_PRECONDITION` / `LEASE_LOST`

#### Scenario: The rejected item is left settleable
- **WHEN** the item rejected in a mixed batch is settled again with its real token
- **THEN** that settlement succeeds

#### Scenario: Settling an unknown message
- **WHEN** a settlement names a `message_id` that does not exist
- **THEN** its per-item result is `NOT_FOUND` / `MESSAGE_NOT_FOUND` and the rest of the batch is applied

#### Scenario: An empty batch
- **WHEN** `SettleMessages` is called with no settlements
- **THEN** the RPC fails with code `INVALID_ARGUMENT` and reason `VALIDATION_FAILED`

#### Scenario: One item exceeds a per-item limit
- **WHEN** a batch contains one settlement whose `retry_delay` exceeds 12 hours alongside one valid settlement
- **THEN** the RPC succeeds, the oversized item's own result is `INVALID_ARGUMENT` / `VALIDATION_FAILED`, and the valid settlement is applied
