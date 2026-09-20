// Command kyu-engine serves the kyu.v1.Kyu API.
//
// It does not serve any of it yet: the contract is registered and every method
// returns UNIMPLEMENTED. What this binary demonstrates today is that the
// contract generates, registers and is reachable over gRPC.
//
// A deployment must also run a kyu.storage.v1.Storage server. The engine does
// not embed one, and there is no default. Nothing here connects to one yet.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	kyuv1 "github.com/sasidakh/kyu/gen/kyu/v1"
	"github.com/sasidakh/kyu/internal/engine"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	if err := run(); err != nil {
		slog.Error("kyu-engine stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	listen := flag.String("listen", envOr("KYU_LISTEN", "127.0.0.1:50051"),
		"address to serve kyu.v1.Kyu on (env KYU_LISTEN)")
	flag.Parse()

	lis, err := net.Listen("tcp", *listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", *listen, err)
	}

	srv := grpc.NewServer()
	kyuv1.RegisterKyuServer(srv, engine.New())

	// Reflection so that grpcurl and buf curl can drive the server without a
	// copy of the contract. It costs nothing and makes the thing explorable.
	reflection.Register(srv)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-stop
		slog.Info("shutting down", "signal", sig.String())
		srv.GracefulStop()
	}()

	slog.Info("serving kyu.v1.Kyu", "address", lis.Addr().String())
	if err := srv.Serve(lis); err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
