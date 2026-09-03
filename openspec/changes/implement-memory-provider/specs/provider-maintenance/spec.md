## ADDED Requirements

### Requirement: The provider reclaims expired leases on its own
The provider SHALL reclaim expired leases without any call to `ReapExpiredLeases`. Reclamation MAY be lazy, applied when the provider next examines the message. No caller SHALL need to trigger a sweep for the provider to behave correctly.

#### Scenario: An expired lease is reclaimed with no administrative call
- **WHEN** a message's lease expires and a consumer polls `LeaseMessages`, with `ReapExpiredLeases` never called
- **THEN** the message becomes leasable again

#### Scenario: Reclamation is visible to statistics without a lease call
- **WHEN** leases expire and `GetQueueStats` is called without any intervening `LeaseMessages`
- **THEN** the expired messages are no longer counted in `leased_count`

### Requirement: The administrative sweep reports consistent counts
`ReapExpiredLeases` SHALL reclaim leases that have been expired for at least `grace_period`, treating a zero `grace_period` as all expired leases. It SHALL report `returned_to_ready` and `moved_to_dead_letter` counts consistent with the expirations that have actually occurred, never exceeding them. It SHALL be idempotent: a repeat call with nothing newly expired SHALL reclaim nothing.

#### Scenario: Reaping after induced expirations
- **WHEN** three messages are leased with a short TTL, the TTL elapses, and `ReapExpiredLeases` is called with a zero `grace_period` in a queue whose `max_deliveries` is well above one
- **THEN** `returned_to_ready` does not exceed three, `moved_to_dead_letter` is zero, and the queue afterwards reports all three messages as ready

#### Scenario: A repeat sweep reclaims nothing
- **WHEN** `ReapExpiredLeases` is called again with nothing newly expired
- **THEN** both `returned_to_ready` and `moved_to_dead_letter` are zero

#### Scenario: Messages at their limit are dead-lettered by the sweep
- **WHEN** `ReapExpiredLeases` reclaims a lease whose message has reached `max_deliveries`
- **THEN** that message is counted in `moved_to_dead_letter` rather than `returned_to_ready`

### Requirement: Provider identity
`GetProviderInfo` SHALL return a non-empty `provider_name`, a non-empty `provider_version`, and `contract_version` equal to `kyu.storage.v1`.

#### Scenario: Identity is reported
- **WHEN** `GetProviderInfo` is called
- **THEN** `contract_version` is `kyu.storage.v1`, `provider_name` is non-empty, and `provider_version` is non-empty

#### Scenario: Identity needs no queue
- **WHEN** `GetProviderInfo` is called against a provider with no queues at all
- **THEN** the call succeeds

### Requirement: The provider's clock is authoritative
All timestamps the provider returns SHALL be on the provider's own clock, and all decisions about visibility and lease expiry SHALL be made against that clock.

#### Scenario: Expiry is decided by the provider
- **WHEN** a caller presents a token whose lease has expired on the provider's clock
- **THEN** the provider rejects it with `LEASE_LOST` regardless of any timestamp the caller holds

### Requirement: Request validation
The provider SHALL reject malformed requests and requests exceeding its documented limits with `INVALID_ARGUMENT` / `VALIDATION_FAILED`. The enforced limits SHALL be: `payload` at most 1 MiB; `attributes` at most 16 entries and 4 KiB serialised, measured as the sum of every key and value in bytes with no allowance for encoding framing; `idempotency_key` at most 256 bytes; `queue_id` non-empty and at most 256 bytes; `settlements` non-empty and at most 1000 items; `retry_delay` at most 12 hours; `lease_ttl` and `RenewLease.extension` at most 12 hours; `batch_size` at most 100; `max_deliveries` at least 1.

The minimum of 1 second on `lease_ttl` and `extension` SHALL NOT be enforced. That limit is stated in §4 in the client API's vocabulary and binds a provider only by analogy, and the §8.1 conformance scenarios lease with a 300 ms TTL; where the analogy contradicts §8.1, §8.1 governs.

#### Scenario: An oversized payload is rejected
- **WHEN** `SaveMessage` carries a payload larger than 1 MiB
- **THEN** the call fails with code `INVALID_ARGUMENT` and reason `VALIDATION_FAILED`

#### Scenario: A queue with a zero delivery limit is rejected
- **WHEN** `EnsureQueue` supplies a configuration whose `max_deliveries` is less than 1
- **THEN** the call fails with code `INVALID_ARGUMENT` and reason `VALIDATION_FAILED`

#### Scenario: A sub-second lease TTL is accepted
- **WHEN** `LeaseMessages` supplies a `lease_ttl` of 300 milliseconds
- **THEN** the call succeeds and the lease expires 300 milliseconds later
