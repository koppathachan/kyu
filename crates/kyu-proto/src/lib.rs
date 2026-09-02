//! Rust types for `kyu.storage.v1`, generated from the normative proto in
//! protocol specification §6.
//!
//! Nothing here is hand-written: the module below is `build.rs` output. It is
//! the wire contract. The in-process binding the conformance suite runs against
//! is the `StorageProvider` trait in `kyu-conformance`; see `kyu-memory` for the
//! adapter that proves the two agree.

#[allow(clippy::all, clippy::pedantic, rustdoc::all)]
pub mod storage {
    include!(concat!(env!("OUT_DIR"), "/kyu.storage.v1.rs"));
}

pub use storage::*;
