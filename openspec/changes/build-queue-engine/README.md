# build-queue-engine

Define kyu's two gRPC contracts — the client API and a storage SPI of eight
operations over opaque items — and build the engine, a reference in-memory
storage server, and the reaper, until a message can be enqueued, leased,
redelivered, dead-lettered and acknowledged end to end.
