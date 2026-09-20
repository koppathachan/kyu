package memory_test

import (
	"context"
	"sync"
	"testing"

	"github.com/sasidakh/kyu/internal/storage/memory"
	storagev1 "github.com/sasidakh/kyu/kyu/storage/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func newTestServer(t *testing.T, partitions ...string) *memory.Server {
	t.Helper()
	s := memory.New()
	for _, name := range partitions {
		if _, err := s.CreatePartition(context.Background(), &storagev1.CreatePartitionRequest{
			Partition: &storagev1.Partition{Name: name},
		}); err != nil {
			t.Fatalf("CreatePartition(%q): %v", name, err)
		}
	}
	return s
}

func put(t *testing.T, s *memory.Server, partition, id string) int64 {
	t.Helper()
	resp, err := s.Put(context.Background(), &storagev1.PutRequest{
		Partition: partition, Id: id, Value: []byte(id),
	})
	if err != nil {
		t.Fatalf("Put(%q, %q): %v", partition, id, err)
	}
	return resp.GetPosition()
}

func claim(t *testing.T, s *memory.Server, partition string, now, holdUntil int64, ceiling uint32) *storagev1.Item {
	t.Helper()
	resp, err := s.Claim(context.Background(), &storagev1.ClaimRequest{
		Partition: partition, Now: now, HoldUntil: holdUntil, AttemptCeiling: ceiling,
	})
	if err != nil {
		t.Fatalf("Claim(%q): %v", partition, err)
	}
	return resp.GetItem()
}

func wantCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	if got := status.Code(err); got != want {
		t.Fatalf("status %s, want %s (error: %v)", got, want, err)
	}
}

// 5.8: the fence is what stops a slow consumer destroying a message another
// consumer is at that moment processing.
func TestDeleteIsRefusedWhenTheHoldHasPassedOn(t *testing.T) {
	s := newTestServer(t, "p")
	put(t, s, "p", "m1")

	first := claim(t, s, "p", 100, 200, 5)
	if first.GetAttempts() != 1 {
		t.Fatalf("first claim at attempt %d, want 1", first.GetAttempts())
	}

	// The first consumer's hold lapses and the message passes to a second.
	second := claim(t, s, "p", 300, 400, 5)
	if second.GetId() != "m1" || second.GetAttempts() != 2 {
		t.Fatalf("second claim returned %q at attempt %d, want m1 at 2", second.GetId(), second.GetAttempts())
	}

	// The first consumer, slow rather than dead, now settles.
	_, err := s.Delete(context.Background(), &storagev1.DeleteRequest{
		Partition: "p", Id: "m1", ExpectedAttempts: first.GetAttempts(),
	})
	wantCode(t, err, codes.FailedPrecondition)

	// Nothing changed: the message is present, at the second consumer's
	// attempt count, still carrying the second consumer's hold. The scan is
	// taken at an instant past that hold, since Scan returns only what has
	// lapsed.
	scanned := scanAll(t, s, "p", 500, 0, 10)
	if len(scanned) != 1 {
		t.Fatalf("%d items present, want 1", len(scanned))
	}
	if scanned[0].GetAttempts() != 2 || scanned[0].GetHoldUntil() != 400 {
		t.Fatalf("item is at attempt %d held until %d, want 2 and 400",
			scanned[0].GetAttempts(), scanned[0].GetHoldUntil())
	}
}

func TestReleaseIsRefusedWhenTheHoldHasPassedOn(t *testing.T) {
	s := newTestServer(t, "p")
	put(t, s, "p", "m1")

	first := claim(t, s, "p", 100, 200, 5)
	claim(t, s, "p", 300, 400, 5) // the hold passes to a second consumer

	_, err := s.Release(context.Background(), &storagev1.ReleaseRequest{
		Partition: "p", Id: "m1", ExpectedAttempts: first.GetAttempts(),
	})
	wantCode(t, err, codes.FailedPrecondition)

	// The second consumer's hold stands: at instant 350 nothing is claimable.
	if it := claim(t, s, "p", 350, 500, 5); it != nil {
		t.Fatalf("claimed %q, want nothing — the refused release cleared a live hold", it.GetId())
	}
}

func TestSettlingTheCurrentHoldSucceeds(t *testing.T) {
	s := newTestServer(t, "p")
	put(t, s, "p", "m1")
	it := claim(t, s, "p", 100, 200, 5)

	if _, err := s.Delete(context.Background(), &storagev1.DeleteRequest{
		Partition: "p", Id: "m1", ExpectedAttempts: it.GetAttempts(),
	}); err != nil {
		t.Fatalf("Delete naming the current attempt: %v", err)
	}
	if got := scanAll(t, s, "p", 1000, 0, 10); len(got) != 0 {
		t.Fatalf("%d items remain, want none", len(got))
	}
}

func TestReleaseDoesNotConsumeAnAttempt(t *testing.T) {
	s := newTestServer(t, "p")
	put(t, s, "p", "m1")
	it := claim(t, s, "p", 100, 200, 5)

	if _, err := s.Release(context.Background(), &storagev1.ReleaseRequest{
		Partition: "p", Id: "m1", ExpectedAttempts: it.GetAttempts(),
	}); err != nil {
		t.Fatalf("Release: %v", err)
	}

	// Claimable at once, at an instant its hold had not yet reached.
	again := claim(t, s, "p", 150, 300, 5)
	if again == nil || again.GetAttempts() != 2 {
		t.Fatalf("claim after release returned %v, want attempt 2", again)
	}
}

// 5.9: Put's refusals.
func TestPutRefusesADuplicateId(t *testing.T) {
	s := newTestServer(t, "p")
	put(t, s, "p", "m1")

	_, err := s.Put(context.Background(), &storagev1.PutRequest{
		Partition: "p", Id: "m1", Value: []byte("second"),
	})
	wantCode(t, err, codes.AlreadyExists)

	got := scanAll(t, s, "p", 1000, 0, 10)
	if len(got) != 1 || string(got[0].GetValue()) != "m1" {
		t.Fatalf("the existing item was modified: %v", got)
	}
}

func TestPutRefusesAMissingPartitionAndDoesNotCreateIt(t *testing.T) {
	s := newTestServer(t)

	_, err := s.Put(context.Background(), &storagev1.PutRequest{Partition: "absent", Id: "m1"})
	wantCode(t, err, codes.NotFound)

	resp, err := s.ListPartitions(context.Background(), &storagev1.ListPartitionsRequest{})
	if err != nil {
		t.Fatalf("ListPartitions: %v", err)
	}
	if len(resp.GetPartitions()) != 0 {
		t.Fatalf("%d partitions exist, want none created", len(resp.GetPartitions()))
	}
}

// 5.9: Move's refusal when the destination already holds that id.
func TestMoveRefusesACollidingIdInTheDestination(t *testing.T) {
	s := newTestServer(t, "src", "dst")
	put(t, s, "src", "m1")
	put(t, s, "dst", "m1")

	_, err := s.Move(context.Background(), &storagev1.MoveRequest{
		SourcePartition: "src", DestinationPartition: "dst", Id: "m1",
	})
	wantCode(t, err, codes.AlreadyExists)

	if got := scanAll(t, s, "src", 1000, 0, 10); len(got) != 1 {
		t.Fatalf("source holds %d items, want the item left where it was", len(got))
	}
	if got := scanAll(t, s, "dst", 1000, 0, 10); len(got) != 1 {
		t.Fatalf("destination holds %d items, want it unmodified", len(got))
	}
}

func TestMoveRefusesAnItemAbsentFromTheSource(t *testing.T) {
	s := newTestServer(t, "src", "dst")

	_, err := s.Move(context.Background(), &storagev1.MoveRequest{
		SourcePartition: "src", DestinationPartition: "dst", Id: "absent",
	})
	wantCode(t, err, codes.NotFound)
}

func TestMoveCarriesTheAttemptCountAndTakesANewPosition(t *testing.T) {
	s := newTestServer(t, "src", "dst")
	put(t, s, "dst", "filler") // so the destination's counter is not at 1
	put(t, s, "src", "m1")
	for i := 0; i < 5; i++ {
		claim(t, s, "src", int64(100*i), int64(100*i+1), 99)
	}

	if _, err := s.Move(context.Background(), &storagev1.MoveRequest{
		SourcePartition: "src", DestinationPartition: "dst", Id: "m1",
	}); err != nil {
		t.Fatalf("Move: %v", err)
	}

	if got := scanAll(t, s, "src", 1000, 0, 10); len(got) != 0 {
		t.Fatalf("source still holds %d items", len(got))
	}
	moved := findItem(t, scanAll(t, s, "dst", 1000, 0, 10), "m1")
	if moved.GetAttempts() != 5 {
		t.Fatalf("attempts %d after the move, want 5", moved.GetAttempts())
	}
	if moved.GetHoldUntil() != 0 || moved.HoldUntil != nil {
		t.Fatalf("the hold travelled with the move: %v", moved.HoldUntil)
	}
	if moved.GetPosition() != 2 {
		t.Fatalf("position %d in the destination, want one the destination assigned", moved.GetPosition())
	}
}

func TestClaimRefusesAnItemAtTheCeilingAndTakesTheNextOne(t *testing.T) {
	s := newTestServer(t, "p")
	put(t, s, "p", "m1")
	put(t, s, "p", "m2")

	for i := 0; i < 3; i++ {
		if it := claim(t, s, "p", int64(100*i), int64(100*i+1), 3); it.GetId() != "m1" {
			t.Fatalf("claim %d returned %q, want m1 — the lowest position", i, it.GetId())
		}
	}

	// m1 is at the ceiling now, so the next claim skips to m2.
	it := claim(t, s, "p", 1000, 1100, 3)
	if it.GetId() != "m2" {
		t.Fatalf("claimed %q, want m2", it.GetId())
	}
}

func TestClaimFindingNothingIsASuccess(t *testing.T) {
	s := newTestServer(t, "p")
	resp, err := s.Claim(context.Background(), &storagev1.ClaimRequest{
		Partition: "p", Now: 1, HoldUntil: 2, AttemptCeiling: 5,
	})
	if err != nil {
		t.Fatalf("Claim on an empty partition: %v", err)
	}
	if resp.GetItem() != nil {
		t.Fatalf("an empty partition returned %v", resp.GetItem())
	}
}

// Eligibility follows the caller's instant, not any clock this server keeps.
func TestEligibilityFollowsTheInstantGiven(t *testing.T) {
	s := newTestServer(t, "p")
	put(t, s, "p", "m1")
	claim(t, s, "p", 100, 1_000_000, 5)

	// An instant before the hold: nothing is eligible, however long has
	// actually elapsed in real time.
	if it := claim(t, s, "p", 999_999, 2_000_000, 5); it != nil {
		t.Fatalf("claimed %q at an instant inside the hold", it.GetId())
	}
	// At the hold instant exactly: eligible.
	if it := claim(t, s, "p", 1_000_000, 2_000_000, 5); it == nil {
		t.Fatal("nothing claimable at the hold instant, which is at or before")
	}
}

func TestScanChangesNothingAndLeavesItemsClaimable(t *testing.T) {
	s := newTestServer(t, "p")
	put(t, s, "p", "m1")
	claim(t, s, "p", 100, 200, 5)

	before := scanAll(t, s, "p", 300, 1, 10)
	if len(before) != 1 || before[0].GetAttempts() != 1 {
		t.Fatalf("scan returned %v, want the one lapsed item at attempt 1", before)
	}

	after := scanAll(t, s, "p", 300, 1, 10)
	if len(after) != 1 || after[0].GetAttempts() != 1 || after[0].GetHoldUntil() != 200 {
		t.Fatalf("the scan modified the item: %v", after)
	}

	if it := claim(t, s, "p", 300, 400, 5); it == nil || it.GetAttempts() != 2 {
		t.Fatalf("claim after scan returned %v, want the item at attempt 2", it)
	}
}

func TestScanRespectsItsLimitAndFloor(t *testing.T) {
	s := newTestServer(t, "p")
	for _, id := range []string{"m1", "m2", "m3"} {
		put(t, s, "p", id)
	}
	// m1 and m2 reach one attempt; m3 stays at zero. The first hold is still
	// standing when the second claim runs, or that claim would return m1
	// again — which is the ordering the queue wants and not what this test is
	// about.
	claim(t, s, "p", 0, 1000, 5)
	claim(t, s, "p", 10, 1010, 5)

	atFloorOne := scanAll(t, s, "p", 2000, 1, 10)
	if len(atFloorOne) != 2 {
		t.Fatalf("%d items at or above attempt 1, want 2", len(atFloorOne))
	}
	if limited := scanAll(t, s, "p", 2000, 1, 1); len(limited) != 1 {
		t.Fatalf("%d items with a limit of 1", len(limited))
	}
}

func TestPositionsIncreaseWithCompletedPuts(t *testing.T) {
	s := newTestServer(t, "p")
	first := put(t, s, "p", "m1")
	second := put(t, s, "p", "m2")
	if second <= first {
		t.Fatalf("positions %d then %d, want the second higher", first, second)
	}
}

// Concurrent claims never return the same item: the count of distinct items
// handed out equals the count of successful claims.
func TestConcurrentClaimsNeverReturnTheSameItem(t *testing.T) {
	const items, claimers = 50, 8
	s := newTestServer(t, "p")
	for i := range items {
		put(t, s, "p", string(rune('a'+i%26))+string(rune('0'+i/26)))
	}

	var mu sync.Mutex
	seen := make(map[string]int)

	var wg sync.WaitGroup
	for range claimers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				resp, err := s.Claim(context.Background(), &storagev1.ClaimRequest{
					Partition: "p", Now: 0, HoldUntil: 1 << 60, AttemptCeiling: 1,
				})
				if err != nil {
					t.Errorf("Claim: %v", err)
					return
				}
				if resp.GetItem() == nil {
					return
				}
				mu.Lock()
				seen[resp.GetItem().GetId()]++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(seen) != items {
		t.Fatalf("%d distinct items claimed, want %d", len(seen), items)
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("item %q was returned to %d claimers", id, n)
		}
	}
}

func TestCreatePartitionRefusesARepeatAndKeepsTheSettings(t *testing.T) {
	s := newTestServer(t)
	if _, err := s.CreatePartition(context.Background(), &storagev1.CreatePartitionRequest{
		Partition: &storagev1.Partition{Name: "p", Settings: []byte("first")},
	}); err != nil {
		t.Fatalf("CreatePartition: %v", err)
	}

	_, err := s.CreatePartition(context.Background(), &storagev1.CreatePartitionRequest{
		Partition: &storagev1.Partition{Name: "p", Settings: []byte("second")},
	})
	wantCode(t, err, codes.AlreadyExists)

	resp, err := s.ListPartitions(context.Background(), &storagev1.ListPartitionsRequest{})
	if err != nil {
		t.Fatalf("ListPartitions: %v", err)
	}
	if len(resp.GetPartitions()) != 1 || string(resp.GetPartitions()[0].GetSettings()) != "first" {
		t.Fatalf("settings are %v, want the first creation's, unchanged", resp.GetPartitions())
	}
}

func TestSettingsAreReturnedByteForByte(t *testing.T) {
	s := memory.New()
	settings := []byte{0x00, 0xff, 0x01, 0x80}
	if _, err := s.CreatePartition(context.Background(), &storagev1.CreatePartitionRequest{
		Partition: &storagev1.Partition{Name: "p", Settings: settings},
	}); err != nil {
		t.Fatalf("CreatePartition: %v", err)
	}
	resp, err := s.ListPartitions(context.Background(), &storagev1.ListPartitionsRequest{})
	if err != nil {
		t.Fatalf("ListPartitions: %v", err)
	}
	got := resp.GetPartitions()[0].GetSettings()
	if string(got) != string(settings) {
		t.Fatalf("settings came back %v, want %v", got, settings)
	}
}

// A held item whose hold lapses with nothing touching it is still there.
// Nothing in this server removes an item but Delete.
func TestNothingButDeleteRemovesAnItem(t *testing.T) {
	s := newTestServer(t, "p")
	put(t, s, "p", "m1")
	claim(t, s, "p", 100, 200, 5)

	// Long past the hold, after scans and a refused settlement.
	scanAll(t, s, "p", 1_000_000, 0, 10)
	_, _ = s.Delete(context.Background(), &storagev1.DeleteRequest{
		Partition: "p", Id: "m1", ExpectedAttempts: 99,
	})

	if got := scanAll(t, s, "p", 1_000_000, 0, 10); len(got) != 1 {
		t.Fatalf("%d items present, want the item still there", len(got))
	}
}

func scanAll(t *testing.T, s *memory.Server, partition string, now int64, floor uint32, limit uint32) []*storagev1.Item {
	t.Helper()
	resp, err := s.Scan(context.Background(), &storagev1.ScanRequest{
		Partition: partition, Now: now, AttemptFloor: floor, Limit: limit,
	})
	if err != nil {
		t.Fatalf("Scan(%q): %v", partition, err)
	}
	return resp.GetItems()
}

func findItem(t *testing.T, items []*storagev1.Item, id string) *storagev1.Item {
	t.Helper()
	for _, it := range items {
		if it.GetId() == id {
			return it
		}
	}
	t.Fatalf("item %q not among %v", id, items)
	return nil
}
