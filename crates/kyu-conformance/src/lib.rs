//! The Kyu v1 storage provider conformance suite.
//!
//! Two things live here, and nothing else:
//!
//! - [`spi`] — the storage provider SPI (protocol specification §6) as a native
//!   Rust trait. A mechanical transcription of the proto; no design decisions.
//! - [`scenarios`] — protocol specification §8.1, the thirteen provider
//!   conformance requirements, as executable checks. This is the definition of
//!   done.
//!
//! Implement [`spi::StorageProvider`], then run every scenario against it.

pub mod scenarios;
pub mod spi;

pub use spi::StorageProvider;
