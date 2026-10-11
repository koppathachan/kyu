# The reference storage server.
#
# This is a fixture and a worked example of kyu.storage.v1.Storage, not a
# production backend: everything it holds is in process memory, so stopping the
# container loses every message. A deployment supplies its own implementation.
#
# The build stage runs on the builder's own platform and cross-compiles for
# the target, so a multi-arch image needs no emulation.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG TARGETOS
ARG TARGETARCH
# The release version, stamped by the release workflow; a local build reports
# what the Go toolchain recorded instead.
ARG VERSION=
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
	-ldflags "-s -w${VERSION:+ -X github.com/koppathachan/kyu/internal/version.version=$VERSION}" \
	-o /out/kyu-memstore ./cmd/kyu-memstore

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/kyu-memstore /kyu-memstore

ENV KYU_STORAGE_LISTEN=0.0.0.0:50052
EXPOSE 50052

USER nonroot:nonroot
ENTRYPOINT ["/kyu-memstore"]
