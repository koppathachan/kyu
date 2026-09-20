// Command kyu-memstore serves the reference kyu.storage.v1.Storage.
//
// It is a reference and a test fixture, not a production backend. Everything
// it holds is in process memory: stop it and every message is gone. It exists
// so that the contract has a working example, and so that kyu's own tests
// reach a backend over real gRPC on a real socket rather than through an
// in-process shortcut that would let a wire-level mistake pass unnoticed.
//
// A deployment supplies its own implementation of kyu.storage.v1.Storage.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/sasidakh/kyu/internal/storage/memory"
	storagev1 "github.com/sasidakh/kyu/kyu/storage/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	if err := run(); err != nil {
		slog.Error("kyu-memstore stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	listen := flag.String("listen", envOr("KYU_STORAGE_LISTEN", "127.0.0.1:50052"),
		"address to serve kyu.storage.v1.Storage on (env KYU_STORAGE_LISTEN)")
	flag.Parse()

	lis, err := net.Listen("tcp", *listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", *listen, err)
	}

	srv := grpc.NewServer()
	storagev1.RegisterStorageServer(srv, memory.New())
	reflection.Register(srv)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-stop
		slog.Info("shutting down", "signal", sig.String())
		srv.GracefulStop()
	}()

	slog.Warn("this is the reference storage server: everything it holds is in memory and is lost on exit")
	slog.Info("serving kyu.storage.v1.Storage", "address", lis.Addr().String())
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
