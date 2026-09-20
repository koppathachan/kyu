package engine

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/sasidakh/kyu/internal/reaper"
)

// Engine-wide defaults. Per-queue policy — hold duration and attempt ceiling —
// is not here: it belongs to the queue and lives in storage. The sweep pacing
// defaults belong to the reaper and are re-exported so that one table in the
// README can name them all.
const (
	DefaultSweepInterval     = reaper.DefaultInterval
	DefaultSweepIntervalMax  = reaper.DefaultIntervalMax
	DefaultScanLimit         = reaper.DefaultScanLimit
	DefaultDiscoveryInterval = 30 * time.Second
	DefaultNegativeCacheTTL  = 5 * time.Second
)

// Config is the engine's deployment-wide configuration.
//
// Every field is read from the environment and may be overridden by a flag.
// The flag wins, which is the usual precedence and the one that lets an
// operator override a container's environment without rebuilding it.
type Config struct {
	// Listen is where the engine serves kyu.v1.Kyu.
	Listen string

	// StorageTarget is the storage server's address. There is no default and
	// no embedded backend: a deployment supplies one.
	StorageTarget string

	// SweepInterval is the floor a sweep backs off from.
	SweepInterval time.Duration

	// SweepIntervalMax is the ceiling a sweep backs off to.
	SweepIntervalMax time.Duration

	// ScanLimit caps each sweep's Scan. The engine always supplies one, so
	// storage is never asked to choose the reaper's throughput.
	ScanLimit uint32

	// DiscoveryInterval is how often partitions are re-enumerated. Without
	// re-enumeration a queue created on another replica is swept by nobody
	// once that replica goes away.
	DiscoveryInterval time.Duration

	// NegativeCacheTTL is how long an absent queue name is remembered. It is
	// bounded and short on purpose: an absence remembered here can be
	// contradicted by a creation there.
	NegativeCacheTTL time.Duration
}

// DefaultConfig is the configuration before the environment is read.
func DefaultConfig() Config {
	return Config{
		Listen:            "127.0.0.1:50051",
		SweepInterval:     DefaultSweepInterval,
		SweepIntervalMax:  DefaultSweepIntervalMax,
		ScanLimit:         DefaultScanLimit,
		DiscoveryInterval: DefaultDiscoveryInterval,
		NegativeCacheTTL:  DefaultNegativeCacheTTL,
	}
}

// LoadConfig reads the environment over the defaults.
//
// A variable that is set but unreadable is an error rather than a fallback:
// a deployment that meant to set a one-second sweep and typed it wrong should
// be told, not quietly given thirty.
func LoadConfig() (Config, error) {
	c := DefaultConfig()
	var err error

	if v := os.Getenv("KYU_LISTEN"); v != "" {
		c.Listen = v
	}
	if v := os.Getenv("KYU_STORAGE"); v != "" {
		c.StorageTarget = v
	}
	if c.SweepInterval, err = envDuration("KYU_SWEEP_INTERVAL", c.SweepInterval); err != nil {
		return c, err
	}
	if c.SweepIntervalMax, err = envDuration("KYU_SWEEP_INTERVAL_MAX", c.SweepIntervalMax); err != nil {
		return c, err
	}
	if c.ScanLimit, err = envUint32("KYU_SCAN_LIMIT", c.ScanLimit); err != nil {
		return c, err
	}
	if c.DiscoveryInterval, err = envDuration("KYU_DISCOVERY_INTERVAL", c.DiscoveryInterval); err != nil {
		return c, err
	}
	if c.NegativeCacheTTL, err = envDuration("KYU_NEGATIVE_CACHE_TTL", c.NegativeCacheTTL); err != nil {
		return c, err
	}
	return c, nil
}

// RegisterFlags adds a flag per setting, each defaulting to what the
// environment produced, so that a flag overrides the environment.
func (c *Config) RegisterFlags(fs *flag.FlagSet) {
	fs.StringVar(&c.Listen, "listen", c.Listen,
		"address to serve kyu.v1.Kyu on (env KYU_LISTEN)")
	fs.StringVar(&c.StorageTarget, "storage", c.StorageTarget,
		"address of the kyu.storage.v1.Storage server this deployment supplies (env KYU_STORAGE)")
	fs.DurationVar(&c.SweepInterval, "sweep-interval", c.SweepInterval,
		"floor a queue's sweep backs off from (env KYU_SWEEP_INTERVAL)")
	fs.DurationVar(&c.SweepIntervalMax, "sweep-interval-max", c.SweepIntervalMax,
		"ceiling a queue's sweep backs off to (env KYU_SWEEP_INTERVAL_MAX)")
	fs.Func("scan-limit",
		fmt.Sprintf("items one sweep's Scan may return (default %d) (env KYU_SCAN_LIMIT)", c.ScanLimit),
		func(s string) error {
			n, err := parseUint32(s)
			if err != nil {
				return err
			}
			c.ScanLimit = n
			return nil
		})
	fs.DurationVar(&c.DiscoveryInterval, "discovery-interval", c.DiscoveryInterval,
		"how often partitions are re-enumerated (env KYU_DISCOVERY_INTERVAL)")
	fs.DurationVar(&c.NegativeCacheTTL, "negative-cache-ttl", c.NegativeCacheTTL,
		"how long an absent queue name is remembered (env KYU_NEGATIVE_CACHE_TTL)")
}

// Validate refuses a configuration that cannot serve.
func (c Config) Validate() error {
	if c.StorageTarget == "" {
		return fmt.Errorf("no storage server configured: set KYU_STORAGE or -storage. " +
			"A deployment supplies a kyu.storage.v1.Storage server; the engine embeds none")
	}
	if c.SweepInterval <= 0 {
		return fmt.Errorf("sweep interval %s is not positive", c.SweepInterval)
	}
	if c.SweepIntervalMax < c.SweepInterval {
		return fmt.Errorf("sweep interval ceiling %s is below the floor %s", c.SweepIntervalMax, c.SweepInterval)
	}
	if c.ScanLimit == 0 {
		return fmt.Errorf("scan limit is zero, which would sweep nothing")
	}
	if c.DiscoveryInterval <= 0 {
		return fmt.Errorf("discovery interval %s is not positive", c.DiscoveryInterval)
	}
	if c.NegativeCacheTTL < 0 {
		return fmt.Errorf("negative cache TTL %s is negative", c.NegativeCacheTTL)
	}
	return nil
}

// sweeperConfig is the pacing this configuration gives every sweep.
func (c Config) sweeperConfig() reaper.Config {
	return reaper.Config{
		Interval:    c.SweepInterval,
		IntervalMax: c.SweepIntervalMax,
		ScanLimit:   c.ScanLimit,
	}
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback, fmt.Errorf("%s=%q is not a duration: %w", key, v, err)
	}
	return d, nil
}

func envUint32(key string, fallback uint32) (uint32, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := parseUint32(v)
	if err != nil {
		return fallback, fmt.Errorf("%s=%q: %w", key, v, err)
	}
	return n, nil
}

func parseUint32(s string) (uint32, error) {
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("not a number in [0, 2^32): %w", err)
	}
	return uint32(n), nil
}
