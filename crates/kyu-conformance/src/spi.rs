//! The Kyu v1 storage provider SPI, as a native Rust trait.
//!
//! This is a mechanical, method-for-method transcription of `kyu.storage.v1
//! .StorageProviderService` (protocol specification §6) into the in-process
//! binding described in §1. No design decisions are encoded here: every type
//! below corresponds to a proto message of the same name, and every trait
//! method to an rpc of the same name.
//!
//! Protobuf scalar mappings used throughout:
//!
//! | proto                         | Rust                          |
//! |-------------------------------|-------------------------------|
//! | `google.protobuf.Duration`    | [`std::time::Duration`]       |
//! | `google.protobuf.Timestamp`   | [`std::time::SystemTime`]     |
//! | `bytes`                       | `Vec<u8>`                     |
//! | `map<string, string>`         | `HashMap<String, String>`     |
//! | optional (omitted) `Duration` | `Option<Duration>`            |

use std::collections::HashMap;
use std::time::{Duration, SystemTime};

/// The contract version every conformant provider reports (§6,
/// `GetProviderInfoResponse.contract_version`).
pub const CONTRACT_VERSION: &str = "kyu.storage.v1";

// ---------------------------------------------------------------------------
// Errors — protocol specification §7.
// ---------------------------------------------------------------------------

/// Canonical gRPC status codes, as used by §7 and by
/// [`SettlementResult::code`] (`google.rpc.Code`; 0 = OK).
pub mod code {
    pub const OK: i32 = 0;
    pub const INVALID_ARGUMENT: i32 = 3;
    pub const NOT_FOUND: i32 = 5;
    pub const ALREADY_EXISTS: i32 = 6;
    pub const RESOURCE_EXHAUSTED: i32 = 8;
    pub const FAILED_PRECONDITION: i32 = 9;
    pub const INTERNAL: i32 = 13;
    pub const UNAVAILABLE: i32 = 14;
}

/// Machine-readable `reason` values carried in `google.rpc.ErrorInfo`,
/// domain `kyu.dev` (§7).
pub mod reason {
    pub const QUEUE_NOT_FOUND: &str = "QUEUE_NOT_FOUND";
    pub const MESSAGE_NOT_FOUND: &str = "MESSAGE_NOT_FOUND";
    pub const LEASE_LOST: &str = "LEASE_LOST";
    pub const QUEUE_CONFIG_CONFLICT: &str = "QUEUE_CONFIG_CONFLICT";
    pub const VALIDATION_FAILED: &str = "VALIDATION_FAILED";
    pub const STORAGE_UNAVAILABLE: &str = "STORAGE_UNAVAILABLE";
    pub const STORAGE_OVERLOADED: &str = "STORAGE_OVERLOADED";
}

/// An SPI-level failure: a canonical gRPC code plus the §7 `reason`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Error {
    pub code: i32,
    pub reason: String,
}

impl Error {
    pub fn new(code: i32, reason: impl Into<String>) -> Self {
        Self {
            code,
            reason: reason.into(),
        }
    }

    pub fn queue_not_found() -> Self {
        Self::new(code::NOT_FOUND, reason::QUEUE_NOT_FOUND)
    }
    pub fn message_not_found() -> Self {
        Self::new(code::NOT_FOUND, reason::MESSAGE_NOT_FOUND)
    }
    pub fn lease_lost() -> Self {
        Self::new(code::FAILED_PRECONDITION, reason::LEASE_LOST)
    }
    pub fn queue_config_conflict() -> Self {
        Self::new(code::ALREADY_EXISTS, reason::QUEUE_CONFIG_CONFLICT)
    }
    pub fn validation_failed() -> Self {
        Self::new(code::INVALID_ARGUMENT, reason::VALIDATION_FAILED)
    }
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{} ({})", self.reason, self.code)
    }
}

impl std::error::Error for Error {}

pub type Result<T> = std::result::Result<T, Error>;

// ---------------------------------------------------------------------------
// Messages — protocol specification §6.
// ---------------------------------------------------------------------------

/// `kyu.storage.v1.MessageState`
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum MessageState {
    #[default]
    Unspecified = 0,
    Ready = 1,
    Leased = 2,
    DeadLetter = 3,
}

/// `kyu.storage.v1.StorageQueueConfig`
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct StorageQueueConfig {
    pub queue_id: String,
    pub default_lease_ttl: Duration,
    pub max_deliveries: i32,
    pub retry_backoff_base: Duration,
    pub retry_backoff_max: Duration,
    pub dedup_window: Duration,
    pub dead_letter_enabled: bool,
}

/// `kyu.storage.v1.EnsureQueueRequest`
#[derive(Debug, Clone)]
pub struct EnsureQueueRequest {
    pub config: StorageQueueConfig,
}

/// `kyu.storage.v1.EnsureQueueResponse`
///
/// If `created == false` the queue already existed and `config` is the STORED
/// one, which may differ from the requested one.
#[derive(Debug, Clone)]
pub struct EnsureQueueResponse {
    pub config: StorageQueueConfig,
    pub created: bool,
}

/// `kyu.storage.v1.GetQueueConfigRequest`
#[derive(Debug, Clone)]
pub struct GetQueueConfigRequest {
    pub queue_id: String,
}

/// `kyu.storage.v1.GetQueueConfigResponse`
#[derive(Debug, Clone)]
pub struct GetQueueConfigResponse {
    pub config: StorageQueueConfig,
}

/// `kyu.storage.v1.ListQueuesRequest`
#[derive(Debug, Clone, Default)]
pub struct ListQueuesRequest {
    pub page_size: i32,
    pub page_token: String,
}

/// `kyu.storage.v1.ListQueuesResponse`
#[derive(Debug, Clone, Default)]
pub struct ListQueuesResponse {
    pub queues: Vec<StorageQueueConfig>,
    pub next_page_token: String,
}

/// `kyu.storage.v1.DeleteQueueRequest`
#[derive(Debug, Clone)]
pub struct DeleteQueueRequest {
    pub queue_id: String,
}

/// `kyu.storage.v1.DeleteQueueResponse`
#[derive(Debug, Clone)]
pub struct DeleteQueueResponse {
    pub purged_message_count: i64,
}

/// `kyu.storage.v1.GetQueueStatsRequest`
#[derive(Debug, Clone)]
pub struct GetQueueStatsRequest {
    pub queue_id: String,
}

/// `kyu.storage.v1.GetQueueStatsResponse`
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct GetQueueStatsResponse {
    pub ready_count: i64,
    pub leased_count: i64,
    pub dead_letter_count: i64,
}

/// `kyu.storage.v1.SaveMessageRequest`
#[derive(Debug, Clone, Default)]
pub struct SaveMessageRequest {
    pub queue_id: String,
    pub payload: Vec<u8>,
    pub priority: i32,
    pub visibility_delay: Option<Duration>,
    /// Empty = no dedup.
    pub idempotency_key: String,
    pub attributes: HashMap<String, String>,
}

/// `kyu.storage.v1.SaveMessageResponse`
#[derive(Debug, Clone)]
pub struct SaveMessageResponse {
    pub message_id: String,
    pub saved_at: SystemTime,
    pub deduplicated: bool,
}

/// `kyu.storage.v1.LeaseMessagesRequest`
#[derive(Debug, Clone)]
pub struct LeaseMessagesRequest {
    pub queue_id: String,
    pub batch_size: i32,
    /// Omit to use the queue's `default_lease_ttl`.
    pub lease_ttl: Option<Duration>,
}

/// `kyu.storage.v1.LeaseMessagesResponse`
#[derive(Debug, Clone, Default)]
pub struct LeaseMessagesResponse {
    pub messages: Vec<LeasedMessage>,
}

/// `kyu.storage.v1.LeasedMessage`
#[derive(Debug, Clone)]
pub struct LeasedMessage {
    pub message_id: String,
    pub queue_id: String,
    pub payload: Vec<u8>,
    pub priority: i32,
    /// Already incremented for this delivery.
    pub delivery_count: i32,
    pub lease_token: String,
    pub lease_expires_at: SystemTime,
    pub attributes: HashMap<String, String>,
}

/// `kyu.storage.v1.RenewLeaseRequest`
#[derive(Debug, Clone)]
pub struct RenewLeaseRequest {
    pub queue_id: String,
    pub message_id: String,
    pub lease_token: String,
    /// New expiry is `now + extension`.
    pub extension: Duration,
}

/// `kyu.storage.v1.RenewLeaseResponse`
#[derive(Debug, Clone)]
pub struct RenewLeaseResponse {
    pub lease_expires_at: SystemTime,
}

/// `kyu.storage.v1.StorageSettlement.Outcome`
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum Outcome {
    #[default]
    Unspecified = 0,
    /// Success -> remove permanently.
    AckDelete = 1,
    /// Nack -> READY after `retry_delay`.
    Retry = 2,
    /// Force to DLQ regardless of `delivery_count`.
    DeadLetter = 3,
}

/// `kyu.storage.v1.StorageSettlement`
#[derive(Debug, Clone)]
pub struct StorageSettlement {
    pub queue_id: String,
    pub message_id: String,
    pub lease_token: String,
    pub outcome: Outcome,
    /// Only meaningful for [`Outcome::Retry`]; zero = retry immediately. The
    /// engine has already applied the queue's backoff policy to this value.
    pub retry_delay: Duration,
    pub dead_letter_reason: String,
}

/// `kyu.storage.v1.SettleMessagesRequest`
#[derive(Debug, Clone)]
pub struct SettleMessagesRequest {
    pub settlements: Vec<StorageSettlement>,
}

/// `kyu.storage.v1.SettlementResult`
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SettlementResult {
    pub message_id: String,
    /// Canonical `google.rpc.Code`; 0 = OK.
    pub code: i32,
    /// e.g. `"LEASE_LOST"`, `"MESSAGE_NOT_FOUND"`.
    pub error_reason: String,
}

/// `kyu.storage.v1.SettleMessagesResponse`
#[derive(Debug, Clone)]
pub struct SettleMessagesResponse {
    /// Same order as the request.
    pub results: Vec<SettlementResult>,
}

/// `kyu.storage.v1.ReapExpiredLeasesRequest`
#[derive(Debug, Clone, Default)]
pub struct ReapExpiredLeasesRequest {
    /// Only reap leases expired for at least this long. Zero = all expired.
    pub grace_period: Duration,
}

/// `kyu.storage.v1.ReapExpiredLeasesResponse`
#[derive(Debug, Clone)]
pub struct ReapExpiredLeasesResponse {
    pub returned_to_ready: i64,
    pub moved_to_dead_letter: i64,
}

/// `kyu.storage.v1.GetProviderInfoRequest`
#[derive(Debug, Clone, Default)]
pub struct GetProviderInfoRequest {}

/// `kyu.storage.v1.GetProviderInfoResponse`
#[derive(Debug, Clone)]
pub struct GetProviderInfoResponse {
    pub provider_name: String,
    pub provider_version: String,
    /// MUST be [`CONTRACT_VERSION`].
    pub contract_version: String,
}

// ---------------------------------------------------------------------------
// The service — protocol specification §6.
// ---------------------------------------------------------------------------

/// `kyu.storage.v1.StorageProviderService`, bound in-process.
///
/// The Queue Engine is the only client of this trait. `Send + Sync` because
/// the engine shares one provider across concurrent callers; see §8.1
/// requirement 8.
pub trait StorageProvider: Send + Sync {
    // --- Queue registry ---
    fn ensure_queue(&self, req: EnsureQueueRequest) -> Result<EnsureQueueResponse>;
    fn get_queue_config(&self, req: GetQueueConfigRequest) -> Result<GetQueueConfigResponse>;
    fn list_queues(&self, req: ListQueuesRequest) -> Result<ListQueuesResponse>;
    fn delete_queue(&self, req: DeleteQueueRequest) -> Result<DeleteQueueResponse>;
    fn get_queue_stats(&self, req: GetQueueStatsRequest) -> Result<GetQueueStatsResponse>;

    // --- Message lifecycle ---
    fn save_message(&self, req: SaveMessageRequest) -> Result<SaveMessageResponse>;
    fn lease_messages(&self, req: LeaseMessagesRequest) -> Result<LeaseMessagesResponse>;
    fn renew_lease(&self, req: RenewLeaseRequest) -> Result<RenewLeaseResponse>;
    /// Batch settle. Each item succeeds or fails independently (§7, batch rule).
    fn settle_messages(&self, req: SettleMessagesRequest) -> Result<SettleMessagesResponse>;

    // --- Maintenance ---
    /// Administrative sweep trigger only. Providers MUST also reap on their own.
    fn reap_expired_leases(
        &self,
        req: ReapExpiredLeasesRequest,
    ) -> Result<ReapExpiredLeasesResponse>;

    fn get_provider_info(&self, req: GetProviderInfoRequest) -> Result<GetProviderInfoResponse>;
}
