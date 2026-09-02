//! Conversions between the generated `kyu.storage.v1` types (the wire form) and
//! the in-process SPI types in `kyu_conformance::spi`.
//!
//! §1: an implementation may bind the SPI in-process or over gRPC, and both are
//! the same contract. These conversions are where that claim is checked.

use std::time::{Duration, SystemTime, UNIX_EPOCH};

/// `std::time::Duration` -> `google.protobuf.Duration`.
pub fn duration_to_proto(duration: Duration) -> prost_types::Duration {
    prost_types::Duration {
        seconds: duration.as_secs() as i64,
        nanos: duration.subsec_nanos() as i32,
    }
}

/// `google.protobuf.Duration` -> `std::time::Duration`.
pub fn duration_from_proto(duration: Option<prost_types::Duration>) -> Option<Duration> {
    let duration = duration?;
    Some(Duration::new(
        duration.seconds as u64,
        duration.nanos as u32,
    ))
}

/// `SystemTime` -> `google.protobuf.Timestamp`.
pub fn timestamp_to_proto(instant: SystemTime) -> prost_types::Timestamp {
    let since_epoch = instant
        .duration_since(UNIX_EPOCH)
        .expect("timestamps are at or after the unix epoch");
    prost_types::Timestamp {
        seconds: since_epoch.as_secs() as i64,
        nanos: since_epoch.subsec_nanos() as i32,
    }
}

/// `google.protobuf.Timestamp` -> `SystemTime`.
pub fn timestamp_from_proto(instant: Option<prost_types::Timestamp>) -> Option<SystemTime> {
    let instant = instant?;
    Some(UNIX_EPOCH + Duration::new(instant.seconds as u64, instant.nanos as u32))
}
