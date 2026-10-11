package spi

import (
	"context"
	"fmt"

	storagev1 "github.com/koppathachan/kyu/kyu/storage/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Client is one connection to a storage server.
//
// Every method is one SPI call, with the backend's status wrapped so that a
// failure names the operation and the partition it happened on. See the
// package comment for what this type deliberately does not do.
type Client struct {
	// target is the backend's address, carried into every error it produces.
	target string

	conn *grpc.ClientConn
	rpc  storagev1.StorageClient
}

// Dial connects to the storage server a deployment supplies.
//
// The connection is lazy, and no handshake is performed. A backend serving
// some other package fails the first real call with UNIMPLEMENTED, which is
// the fail-fast a ping would have provided.
func Dial(target string, opts ...grpc.DialOption) (*Client, error) {
	if target == "" {
		return nil, fmt.Errorf("no storage target given")
	}
	if len(opts) == 0 {
		opts = []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	}
	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, fmt.Errorf("connect to storage at %s: %w", target, err)
	}
	return &Client{target: target, conn: conn, rpc: storagev1.NewStorageClient(conn)}, nil
}

// Close releases the connection.
func (c *Client) Close() error { return c.conn.Close() }

// Target is the backend's address, as configured.
func (c *Client) Target() string { return c.target }

func (c *Client) CreatePartition(ctx context.Context, name string, settings []byte) error {
	_, err := c.rpc.CreatePartition(ctx, &storagev1.CreatePartitionRequest{
		Partition: &storagev1.Partition{Name: name, Settings: settings},
	})
	return wrap(c.target, "CreatePartition", name, err)
}

func (c *Client) ListPartitions(ctx context.Context) ([]*storagev1.Partition, error) {
	resp, err := c.rpc.ListPartitions(ctx, &storagev1.ListPartitionsRequest{})
	if err != nil {
		return nil, wrap(c.target, "ListPartitions", "", err)
	}
	return resp.GetPartitions(), nil
}

func (c *Client) Put(ctx context.Context, partition, id string, value []byte) (int64, error) {
	resp, err := c.rpc.Put(ctx, &storagev1.PutRequest{Partition: partition, Id: id, Value: value})
	if err != nil {
		return 0, wrap(c.target, "Put", partition, err)
	}
	return resp.GetPosition(), nil
}

// Claim returns a nil item when nothing was eligible, which is a success.
func (c *Client) Claim(ctx context.Context, partition string, now, holdUntil int64, ceiling uint32) (*storagev1.Item, error) {
	resp, err := c.rpc.Claim(ctx, &storagev1.ClaimRequest{
		Partition: partition, Now: now, HoldUntil: holdUntil, AttemptCeiling: ceiling,
	})
	if err != nil {
		return nil, wrap(c.target, "Claim", partition, err)
	}
	return resp.GetItem(), nil
}

func (c *Client) Release(ctx context.Context, partition, id string, expectedAttempts uint32) error {
	_, err := c.rpc.Release(ctx, &storagev1.ReleaseRequest{
		Partition: partition, Id: id, ExpectedAttempts: expectedAttempts,
	})
	return wrap(c.target, "Release", partition, err)
}

func (c *Client) Delete(ctx context.Context, partition, id string, expectedAttempts uint32) error {
	_, err := c.rpc.Delete(ctx, &storagev1.DeleteRequest{
		Partition: partition, Id: id, ExpectedAttempts: expectedAttempts,
	})
	return wrap(c.target, "Delete", partition, err)
}

func (c *Client) Move(ctx context.Context, source, destination, id string) error {
	_, err := c.rpc.Move(ctx, &storagev1.MoveRequest{
		SourcePartition: source, DestinationPartition: destination, Id: id,
	})
	return wrap(c.target, "Move", source, err)
}

func (c *Client) Scan(ctx context.Context, partition string, now int64, floor, limit uint32) ([]*storagev1.Item, error) {
	resp, err := c.rpc.Scan(ctx, &storagev1.ScanRequest{
		Partition: partition, Now: now, AttemptFloor: floor, Limit: limit,
	})
	if err != nil {
		return nil, wrap(c.target, "Scan", partition, err)
	}
	return resp.GetItems(), nil
}
