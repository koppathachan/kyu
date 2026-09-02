//! Conversions between the generated `kyu.storage.v1` types (the wire form) and
//! the in-process SPI types in `kyu_conformance::spi`.
//!
//! §1: an implementation may bind the SPI in-process or over gRPC, and both are
//! the same contract. These conversions are where that claim is checked.

use std::time::Duration;

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
