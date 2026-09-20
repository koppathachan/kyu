# The reference storage server.
#
# This is a fixture and a worked example of kyu.storage.v1.Storage, not a
# production backend: everything it holds is in process memory, so stopping the
# container loses every message. A deployment supplies its own implementation.
FROM golang:1.27-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /out/kyu-memstore ./cmd/kyu-memstore

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/kyu-memstore /kyu-memstore

ENV KYU_STORAGE_LISTEN=0.0.0.0:50052
EXPOSE 50052

USER nonroot:nonroot
ENTRYPOINT ["/kyu-memstore"]
