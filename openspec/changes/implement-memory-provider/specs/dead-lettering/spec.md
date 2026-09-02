## ADDED Requirements

### Requirement: A message is never delivered past its limit
`max_deliveries` SHALL be the maximum number of times a message may be delivered. A message whose `delivery_count` has reached `max_deliveries` SHALL NOT be delivered again, whichever route made it eligible.

#### Scenario: The limit is reached by nack
- **WHEN** a queue with `max_deliveries = 2` holds a message that has been delivered and nacked twice
- **THEN** no further delivery occurs, however long a consumer polls

#### Scenario: The limit is reached by lease expiry
- **WHEN** a queue with `max_deliveries = 1` holds a message whose single lease has expired without settlement
- **THEN** no further delivery occurs

#### Scenario: Below the limit the message is redelivered
- **WHEN** a queue with `max_deliveries = 5` holds a message that has been delivered once and nacked
- **THEN** the message is delivered again with `delivery_count = 2`

### Requirement: Reaching the limit dead-letters the message
When a message that has reached `max_deliveries` would otherwise become eligible for delivery, whether through a nack or through lease expiry, the provider SHALL move it to `DEAD_LETTER` instead. A dead-lettered message SHALL stay in its own queue, SHALL never be delivered, and SHALL be counted in `dead_letter_count` rather than in `ready_count` or `leased_count`.

#### Scenario: Dead-lettered after a nack at the limit
- **WHEN** a message in a queue with `max_deliveries = 2` is nacked on its second delivery
- **THEN** `GetQueueStats` reports `dead_letter_count = 1` with `ready_count = 0` and `leased_count = 0`

#### Scenario: Dead-lettered after expiry at the limit
- **WHEN** a message in a queue with `max_deliveries = 1` has its only lease expire
- **THEN** `GetQueueStats` reports `dead_letter_count = 1` and the message is not leasable

#### Scenario: A dead-lettered message is terminal
- **WHEN** a lease is attempted against a queue whose only message is in `DEAD_LETTER`
- **THEN** no message is returned

### Requirement: Dead-lettering may be observed lazily
The transition to `DEAD_LETTER` MAY be applied when the provider next examines the message rather than at the instant of expiry, and queue statistics MAY therefore lag by at most the provider's sweep interval. A delivery beyond `max_deliveries` SHALL NOT occur under any circumstances.

#### Scenario: Statistics settle once the provider examines the message
- **WHEN** a message reaches its limit through lease expiry and the provider is next asked to lease from or report on that queue
- **THEN** the message is counted in `dead_letter_count` and was never delivered again

### Requirement: Dead-lettering can be disabled
When the queue's `dead_letter_enabled` is false, a message that would be dead-lettered SHALL be deleted instead.

#### Scenario: A message at the limit is dropped
- **WHEN** a queue with `max_deliveries = 1` and `dead_letter_enabled = false` holds a message that has been delivered and nacked once
- **THEN** the message is not redelivered and `GetQueueStats` reports `(0, 0, 0)`

### Requirement: Forced dead-lettering ignores the delivery count
A settlement with outcome `OUTCOME_DEAD_LETTER` against a live lease SHALL move the message to `DEAD_LETTER` regardless of its `delivery_count`, subject to `dead_letter_enabled`. The supplied `dead_letter_reason` SHALL be retained with the message.

#### Scenario: Forced below the limit
- **WHEN** a message on its first delivery in a queue with `max_deliveries = 5` is settled with `OUTCOME_DEAD_LETTER`
- **THEN** the settlement succeeds, `GetQueueStats` reports `dead_letter_count = 1`, and the message is never delivered again

#### Scenario: Forced with dead-lettering disabled
- **WHEN** a message is settled with `OUTCOME_DEAD_LETTER` in a queue whose `dead_letter_enabled` is false
- **THEN** the message is deleted instead and the queue reports `(0, 0, 0)`
