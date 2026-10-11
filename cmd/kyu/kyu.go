// Command kyu is the queue engine. It serves the kyu.v1.Kyu API.
//
// A deployment must also run a kyu.storage.v1.Storage server and point this
// binary at it with -storage or KYU_STORAGE. The engine embeds no backend and
// there is no default: durability is the deployment's to supply.
//
// The reference server in cmd/kyu-memstore is a fixture, not a backend.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/koppathachan/kyu/internal/engine"
	"github.com/koppathachan/kyu/internal/version"
	kyuv1 "github.com/koppathachan/kyu/kyu/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	if err := run(); err != nil {
		slog.Error("kyu stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// The environment is read first and the flags default to what it
	// produced, so a flag overrides the environment.
	cfg, err := engine.LoadConfig()
	if err != nil {
		return err
	}
	cfg.RegisterFlags(flag.CommandLine)
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("kyu", version.String())
		return nil
	}

	eng, err := engine.New(cfg)
	if err != nil {
		return err
	}
	defer eng.Close()

	// Enumerate what storage already holds, so that queues created before
	// this process started — here or on another replica — are swept here too.
	// A backend that cannot be reached yet is not fatal: the engine serves,
	// and a client naming a queue triggers a lookup of its own.
	if err := eng.Start(context.Background()); err != nil {
		slog.Warn("could not enumerate partitions at startup; sweeps will begin as queues are discovered",
			"error", err)
	}

	lis, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Listen, err)
	}

	srv := grpc.NewServer()
	kyuv1.RegisterKyuServer(srv, eng)

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

	slog.Info("serving kyu.v1.Kyu", "version", version.String(), "address", lis.Addr().String(), "storage", cfg.StorageTarget)
	if err := srv.Serve(lis); err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}
