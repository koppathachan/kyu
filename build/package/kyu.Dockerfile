# kyu, the queue engine. It serves kyu.v1.Kyu and reaches a storage server the deployment
# supplies — there is no embedded backend, so KYU_STORAGE must be set.
#
# Built from source in one stage and shipped in another, so the image carries
# the binary and nothing that built it.
#
# The build stage runs on the builder's own platform and cross-compiles for
# the target, so a multi-arch image needs no emulation.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
WORKDIR /src

# Dependencies first, so a source-only change does not re-download them.
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
	-o /out/kyu ./cmd/kyu

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/kyu /kyu

ENV KYU_LISTEN=0.0.0.0:50051
EXPOSE 50051

USER nonroot:nonroot
ENTRYPOINT ["/kyu"]
