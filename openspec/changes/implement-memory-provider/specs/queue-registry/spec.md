## ADDED Requirements

### Requirement: Idempotent queue creation
The provider SHALL create a queue on the first `EnsureQueue` for a `queue_id` and report `created = true`. A subsequent `EnsureQueue` for the same `queue_id` SHALL report `created = false` and return the **stored** configuration, whether or not it matches the requested one. The provider SHALL NOT raise `QUEUE_CONFIG_CONFLICT`; comparing configurations is the engine's responsibility (§6, §8.2).

#### Scenario: First creation
- **WHEN** `EnsureQueue` is called with a `queue_id` that does not exist
- **THEN** the queue is created with the supplied configuration and the response has `created = true` and that same configuration

#### Scenario: Repeat creation with an identical config
- **WHEN** `EnsureQueue` is called again with the same `queue_id` and the same configuration
- **THEN** the response has `created = false` and no second queue is created

#### Scenario: Repeat creation with a differing config
- **WHEN** `EnsureQueue` is called again with the same `queue_id` but a different configuration
- **THEN** the response has `created = false`, carries the originally stored configuration, and the call succeeds rather than failing with `ALREADY_EXISTS`

### Requirement: Queue configuration lookup
The provider SHALL return the stored configuration for a known `queue_id` and SHALL fail with `NOT_FOUND` / `QUEUE_NOT_FOUND` for an unknown one.

#### Scenario: Known queue
- **WHEN** `GetQueueConfig` is called for a queue that exists
- **THEN** the stored `StorageQueueConfig` is returned

#### Scenario: Unknown queue
- **WHEN** `GetQueueConfig` is called for a `queue_id` that was never created
- **THEN** the call fails with code `NOT_FOUND` and reason `QUEUE_NOT_FOUND`

### Requirement: Queue listing with stable pagination
The provider SHALL list queues ordered by `queue_id` ascending. When `page_size` is greater than zero the provider SHALL return at most that many queues and set `next_page_token` to the last `queue_id` returned, or to the empty string when the page is the last one. A `page_size` of zero SHALL return all queues.

#### Scenario: Listing every queue
- **WHEN** `ListQueues` is called with `page_size = 0`
- **THEN** every queue is returned, ordered by `queue_id` ascending, and `next_page_token` is empty

#### Scenario: Paging through queues
- **WHEN** `ListQueues` is called with a `page_size` smaller than the number of queues, and then called again with the returned `next_page_token`
- **THEN** the second page continues after the last queue of the first page, with no queue repeated or skipped

### Requirement: Queue statistics
The provider SHALL report `ready_count`, `leased_count` and `dead_letter_count` for a queue, counting each stored message under exactly one of the three. Statistics SHALL be computed from the same state that serves leases and settlements, so that a message under a live lease is never absent from all three counts.

#### Scenario: Counts track the message lifecycle
- **WHEN** one message is saved, then leased, then acked
- **THEN** `GetQueueStats` reports `(1, 0, 0)` after the save, `(0, 1, 0)` while the lease is live, and `(0, 0, 0)` after the ack

#### Scenario: Unknown queue
- **WHEN** `GetQueueStats` is called for a `queue_id` that does not exist
- **THEN** the call fails with code `NOT_FOUND` and reason `QUEUE_NOT_FOUND`

### Requirement: Queue isolation
A message SHALL belong to exactly one queue for its whole lifetime. `LeaseMessages` SHALL only ever return messages from the requested queue, and deduplication SHALL be scoped to a single queue.

#### Scenario: A lease sees only its own queue
- **WHEN** two queues each hold messages and `LeaseMessages` is called on one of them with a `batch_size` larger than that queue's contents
- **THEN** only messages whose `queue_id` matches the request are returned

#### Scenario: The same idempotency key in two queues
- **WHEN** the same `idempotency_key` is used to save into two different queues
- **THEN** two distinct messages are stored and neither save is flagged `deduplicated`

### Requirement: Queue deletion purges and reports
`DeleteQueue` SHALL remove the queue, its messages in every state, and its deduplication records, and SHALL report the number of messages purged. After deletion the `queue_id` SHALL be treated as unknown.

#### Scenario: Deletion reports the purged count
- **WHEN** a queue holding two messages is deleted
- **THEN** `purged_message_count` is 2

#### Scenario: A deleted queue is gone
- **WHEN** `GetQueueStats` is called for a queue that was just deleted
- **THEN** the call fails with code `NOT_FOUND` and reason `QUEUE_NOT_FOUND`

#### Scenario: Deletion leaves other queues untouched
- **WHEN** one of two queues is deleted
- **THEN** the other queue's messages and statistics are unchanged

### Requirement: Queue-scoped requests validate their queue
Every request carrying a `queue_id` SHALL fail with `NOT_FOUND` / `QUEUE_NOT_FOUND` when that queue does not exist, including `SaveMessage`, `LeaseMessages` and `RenewLease`.

#### Scenario: Saving into an unknown queue
- **WHEN** `SaveMessage` names a `queue_id` that was never created
- **THEN** the call fails with code `NOT_FOUND` and reason `QUEUE_NOT_FOUND`
