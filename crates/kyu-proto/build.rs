//! Compiles the normative `kyu.storage.v1` proto (protocol specification §6).
//!
//! `protox` is a pure-Rust protobuf compiler, so no system `protoc` is needed.

use std::path::PathBuf;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let spec_root = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../spec");
    let proto = spec_root.join("kyu/storage/v1/storage.proto");

    println!("cargo:rerun-if-changed={}", proto.display());

    let descriptors = protox::compile([&proto], [&spec_root])?;
    tonic_prost_build::configure()
        .build_client(false)
        .build_server(true)
        .compile_fds(descriptors)?;

    Ok(())
}
