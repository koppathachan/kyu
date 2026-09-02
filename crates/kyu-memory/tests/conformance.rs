//! Kyu v1 protocol specification §8.1, run against this crate's provider.
//!
//! DO NOT EDIT. This file and the `kyu-conformance` crate are the fixed oracle
//! for this exercise. Changing either invalidates the result. If a scenario
//! looks wrong, say so — do not adjust it.
//!
//! Off by default so it does not get in the way while you are working:
//!
//! ```text
//! cargo test -p kyu-memory --features conformance
//! ```
//!
//! It expects one public entry point:
//!
//! ```ignore
//! kyu_memory::MemoryProvider::new() -> impl kyu_conformance::StorageProvider
//! ```
#![cfg(feature = "conformance")]

use kyu_conformance::scenarios;
use kyu_memory::MemoryProvider;

macro_rules! conformance {
    ($($name:ident => $scenario:path),* $(,)?) => {
        $(
            #[test]
            fn $name() {
                $scenario(&MemoryProvider::new());
            }
        )*
    };
}

conformance! {
    r01_lifecycle        => scenarios::r01_lifecycle,
    r02_deduplication    => scenarios::r02_deduplication,
    r03_lease_expiry     => scenarios::r03_lease_expiry,
    r04_stale_token      => scenarios::r04_stale_token,
    r05_retry_delay      => scenarios::r05_retry_delay,
    r06_dead_lettering   => scenarios::r06_dead_lettering,
    r07_priority         => scenarios::r07_priority,
    r08_concurrency      => scenarios::r08_concurrency,
    r09_batch_settle     => scenarios::r09_batch_settle,
    r10_visibility_delay => scenarios::r10_visibility_delay,
    r11_queue_isolation  => scenarios::r11_queue_isolation,
    r12_reaping          => scenarios::r12_reaping,
    r13_provider_info    => scenarios::r13_provider_info,
}
