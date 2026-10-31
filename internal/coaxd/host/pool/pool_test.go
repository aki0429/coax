// Copyright 2026 Till0196
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later

package pool

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Till0196/coax/internal/coaxd/device"
	"github.com/Till0196/coax/pkg/usbio"
)

// fakeRegistry stands in for the board registry, so the pool can run without
// hardware.
type fakeRegistry struct {
	mutex    sync.Mutex
	detected []Detected
	boards   map[string]*fakeBoard
	// opens tells a board opened once and shared from one opened repeatedly.
	opens   int
	openErr error
}

func (r *fakeRegistry) Detect() ([]Detected, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return append([]Detected(nil), r.detected...), nil
}

func (r *fakeRegistry) Open(one Detected, options device.Options) (device.Board, error) {
	r.mutex.Lock()
	r.opens++
	err := r.openErr
	board := r.boards[options.Selector]
	r.mutex.Unlock()
	if err != nil {
		return nil, err
	}
	if board == nil {
		return nil, errors.New("no such board")
	}
	board.opened.Add(1)
	if board.card {
		return cardBoard{fakeBoard: board}, nil
	}
	return board, nil
}

func (r *fakeRegistry) openCount() int {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return r.opens
}

// fakeBoard is one board with a fixed set of tuners and, optionally, a card.
type fakeBoard struct {
	tuners []device.TunerInfo
	card   bool

	mutex   sync.Mutex
	opened  counter
	closed  counter
	taken   map[int]bool
	tuneErr error
	// streams makes the tuners hand out a stream, and events records what was
	// closed in what order.
	streams bool
	events  []string
}

func (b *fakeBoard) record(event string) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.events = append(b.events, event)
}

func (b *fakeBoard) recorded() []string {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return append([]string(nil), b.events...)
}

func (b *fakeBoard) closedCount() int { return b.closed.Get() }

// failTuningWith makes every tune fail, for the tests about hardware that goes
// away while in use.
func (b *fakeBoard) failTuningWith(err error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.tuneErr = err
}

type counter struct {
	mutex sync.Mutex
	value int
}

func (c *counter) Add(n int) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.value += n
}

func (c *counter) Get() int {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.value
}

func (b *fakeBoard) Name() string               { return "fake" }
func (b *fakeBoard) Tuners() []device.TunerInfo { return b.tuners }

func (b *fakeBoard) OpenTuner(index int) (device.Tuner, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	if b.taken == nil {
		b.taken = map[int]bool{}
	}
	if b.taken[index] {
		return nil, device.ErrTunerBusy
	}
	b.taken[index] = true
	return &fakeDevice{board: b, index: index}, nil
}

func (b *fakeBoard) Close() error {
	b.closed.Add(1)
	b.record("board closed")
	return nil
}

type fakeDevice struct {
	board *fakeBoard
	index int
}

func (d *fakeDevice) Name() string         { return "fake tuner" }
func (d *fakeDevice) Supports(string) bool { return true }
func (d *fakeDevice) Tune(map[string]string) error {
	d.board.mutex.Lock()
	defer d.board.mutex.Unlock()
	return d.board.tuneErr
}
func (d *fakeDevice) Status() (device.Status, error) { return device.Status{Locked: true}, nil }
func (d *fakeDevice) OpenStream() (device.Stream, error) {
	d.board.mutex.Lock()
	defer d.board.mutex.Unlock()
	if !d.board.streams {
		return nil, errors.New("no stream")
	}
	return &fakeStream{board: d.board}, nil
}

func (d *fakeDevice) Close() error {
	d.board.mutex.Lock()
	defer d.board.mutex.Unlock()
	delete(d.board.taken, d.index)
	d.board.events = append(d.board.events, "tuner closed")
	return nil
}

// fakeStream has nothing to read and says when it is closed.
type fakeStream struct{ board *fakeBoard }

func (s *fakeStream) Read(context.Context, []byte) (int, error) { return 0, device.ErrWouldBlock }

func (s *fakeStream) Close() error {
	s.board.record("stream closed")
	return nil
}

// cardBoard also carries a card reader, as a real board does through a wrapper
// around the same handle.
type cardBoard struct{ *fakeBoard }

func (b cardBoard) CardStatus() (device.CardStatus, error) {
	return device.CardStatus{Present: true, Reader: "fake"}, nil
}
func (b cardBoard) ResetCard() ([]byte, error)          { return []byte{0x3b}, nil }
func (b cardBoard) TransmitAPDU([]byte) ([]byte, error) { return []byte{0x90, 0x00}, nil }

// px4Board is the shape this pool exists for: four tuners and a card slot on one
// USB device.
func px4Board() *fakeBoard {
	return &fakeBoard{
		tuners: []device.TunerInfo{
			{Index: 0, Name: "RT710", Deliveries: []string{"ISDB-S"}},
			{Index: 1, Name: "RT710", Deliveries: []string{"ISDB-S"}},
			{Index: 2, Name: "R850", Deliveries: []string{"ISDB-T"}},
			{Index: 3, Name: "R850", Deliveries: []string{"ISDB-T"}},
		},
		card: true,
	}
}

// newTestPool wires a pool over one px4-shaped board.
func newTestPool(t *testing.T, options Options) (*Supervisor, *fakeRegistry, *fakeBoard) {
	t.Helper()
	board := px4Board()
	registry := &fakeRegistry{
		detected: []Detected{{
			Backend:     "px4",
			Description: "PLEX PX-W3U4",
			Selector:    "001:007",
			Location:    "001:007",
			Serial:      "ABC123",
			Port:        "1-2",
			USB:         usbio.ID{Vendor: 0x0511, Product: 0x083f},
			Deliveries:  []string{"ISDB-T", "ISDB-S"},
			Tuners:      board.tuners,
		}},
		boards: map[string]*fakeBoard{"001:007": board},
	}
	pool := New(registry, options)
	t.Cleanup(func() { _ = pool.Close() })
	return pool, registry, board
}

func TestBoardsOpensNothing(t *testing.T) {
	pool, registry, _ := newTestPool(t, Options{})

	boards, err := pool.Boards()
	if err != nil {
		t.Fatalf("list boards: %v", err)
	}
	if registry.openCount() != 0 {
		t.Errorf("listing opened %d boards; listing must not power hardware up", registry.openCount())
	}
	if len(boards) != 1 {
		t.Fatalf("listed %d boards, want 1", len(boards))
	}
	one := boards[0]
	// A shared-serial backend is named by serial and socket together.
	if one.Name != "px4:ABC123@1-2" {
		t.Errorf("named the board %q", one.Name)
	}
	if one.Open {
		t.Error("the board is reported open when nothing has opened it")
	}
	if one.Probed {
		t.Error("the tuners are reported as read from the hardware")
	}
	if len(one.Tuners) != 4 {
		t.Fatalf("described %d tuners, want 4", len(one.Tuners))
	}
	if one.Tuners[0].ID != "px4:ABC123@1-2:0" {
		t.Errorf("named the first tuner %q", one.Tuners[0].ID)
	}
}

// TestOneBoardIsOpenedOnceAndShared is the point of the package: one enclosure's
// four tuners and card are one USB device, and only one handle may exist.
func TestOneBoardIsOpenedOnceAndShared(t *testing.T) {
	pool, registry, _ := newTestPool(t, Options{})
	owner := pool.NewOwner()

	first, err := pool.Lease(Want{DeliverySystem: "ISDB-T", Owner: owner})
	if err != nil {
		t.Fatalf("take the first tuner: %v", err)
	}
	second, err := pool.Lease(Want{DeliverySystem: "ISDB-T", Owner: owner})
	if err != nil {
		t.Fatalf("take the second tuner: %v", err)
	}
	card, err := pool.Card("px4:ABC123@1-2", owner)
	if err != nil {
		t.Fatalf("take the card: %v", err)
	}

	if got := registry.openCount(); got != 1 {
		t.Errorf("opened the board %d times; two tuners and a card share one handle", got)
	}
	if first.Index() == second.Index() {
		t.Errorf("handed tuner %d out twice", first.Index())
	}
	if card.Reader() == nil {
		t.Error("the card hold carries no reader")
	}
	first.Release()
	second.Release()
	card.Release()
}

func TestNamedTunerIsTakenExactly(t *testing.T) {
	pool, _, _ := newTestPool(t, Options{})
	owner := pool.NewOwner()

	lease, err := pool.Lease(Want{TunerID: "px4:ABC123@1-2:3", Owner: owner})
	if err != nil {
		t.Fatalf("take tuner 3: %v", err)
	}
	defer lease.Release()
	if lease.Index() != 3 {
		t.Errorf("took tuner %d, want 3", lease.Index())
	}

	// Somebody else naming a tuner that is held is refused.
	other := pool.NewOwner()
	if _, err := pool.Lease(Want{TunerID: "px4:ABC123@1-2:3", Owner: other}); !errors.Is(err, device.ErrTunerBusy) {
		t.Errorf("another holder took a tuner that was already held: %v", err)
	}
	if _, err := pool.Lease(Want{TunerID: "px4:ABC123@1-2:9", Owner: owner}); !errors.Is(err, ErrUnknownTuner) {
		t.Errorf("took a tuner that does not exist: %v", err)
	}
	if _, err := pool.Lease(Want{TunerID: "nonesuch:0", Owner: owner}); !errors.Is(err, ErrUnknownTuner) {
		t.Errorf("took a tuner on a board that is not there: %v", err)
	}
}

// TestNamingYourOwnTunerGivesItBack lets a client work down a list of channels
// retuning the tuner it holds, without winning it back between channels.
func TestNamingYourOwnTunerGivesItBack(t *testing.T) {
	pool, _, _ := newTestPool(t, Options{})
	owner := pool.NewOwner()

	first, err := pool.Lease(Want{DeliverySystem: "ISDB-T", Owner: owner})
	if err != nil {
		t.Fatalf("take a tuner: %v", err)
	}
	again, err := pool.Lease(Want{TunerID: first.ID(), Owner: owner})
	if err != nil {
		t.Fatalf("name the tuner already held: %v", err)
	}
	if again != first {
		t.Error("naming a held tuner produced a second lease rather than the one held")
	}
	if pool.Held(owner) != 1 {
		t.Errorf("the holder now has %d things, want 1", pool.Held(owner))
	}
}

// TestAnUnnamedRequestNeverReusesATuner is the other half of the rule: "a tuner
// that can do this" means a free one, since a client recording two programmes
// asks exactly that way twice.
func TestAnUnnamedRequestNeverReusesATuner(t *testing.T) {
	pool, _, _ := newTestPool(t, Options{})
	owner := pool.NewOwner()

	first, err := pool.Lease(Want{DeliverySystem: "ISDB-T", Owner: owner})
	if err != nil {
		t.Fatalf("take the first tuner: %v", err)
	}
	second, err := pool.Lease(Want{DeliverySystem: "ISDB-T", Owner: owner})
	if err != nil {
		t.Fatalf("take a second tuner for the same holder: %v", err)
	}
	if first.Index() == second.Index() {
		t.Errorf("one holder recording twice was given tuner %d both times", first.Index())
	}
}

func TestLeaseReportsWhyItCouldNotBeServed(t *testing.T) {
	pool, _, _ := newTestPool(t, Options{})
	owner := pool.NewOwner()

	// Nothing on this board reaches DVB-T2.
	if _, err := pool.Lease(Want{DeliverySystem: "DVB-T2", Owner: owner}); !errors.Is(err, ErrNoTuner) {
		t.Errorf("asked for a system nothing reaches: %v, want %v", err, ErrNoTuner)
	}

	// Both terrestrial tuners taken: could have been served but cannot be now,
	// a different answer.
	first, _ := pool.Lease(Want{DeliverySystem: "ISDB-T", Owner: owner})
	second, _ := pool.Lease(Want{DeliverySystem: "ISDB-T", Owner: owner})
	defer first.Release()
	defer second.Release()
	if _, err := pool.Lease(Want{DeliverySystem: "ISDB-T", Owner: owner}); !errors.Is(err, ErrBusy) {
		t.Errorf("asked for a third terrestrial tuner: %v, want %v", err, ErrBusy)
	}
}

// TestBoardLingersAfterItsLastUser: a board is not shut as a scan moves between
// channels, which would reload firmware every time.
func TestBoardLingersAfterItsLastUser(t *testing.T) {
	pool, registry, board := newTestPool(t, Options{Linger: 50 * time.Millisecond})
	owner := pool.NewOwner()

	lease, err := pool.Lease(Want{DeliverySystem: "ISDB-T", Owner: owner})
	if err != nil {
		t.Fatalf("take a tuner: %v", err)
	}
	lease.Release()

	if board.closed.Get() != 0 {
		t.Error("the board was closed the instant its last tuner went")
	}
	// Something arriving inside the window reuses the open board.
	again, err := pool.Lease(Want{DeliverySystem: "ISDB-T", Owner: owner})
	if err != nil {
		t.Fatalf("take a tuner again: %v", err)
	}
	if got := registry.openCount(); got != 1 {
		t.Errorf("opened the board %d times; the second request should have reused it", got)
	}
	again.Release()

	deadline := time.Now().Add(2 * time.Second)
	for board.closed.Get() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the board was never closed after its linger expired")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestZeroLingerClosesAtOnce(t *testing.T) {
	pool, _, board := newTestPool(t, Options{Linger: 0})
	owner := pool.NewOwner()
	lease, err := pool.Lease(Want{DeliverySystem: "ISDB-T", Owner: owner})
	if err != nil {
		t.Fatalf("take a tuner: %v", err)
	}
	lease.Release()
	if board.closed.Get() != 1 {
		t.Errorf("closed the board %d times; a zero linger closes at once", board.closed.Get())
	}
}

func TestLingerForeverKeepsTheBoardOpen(t *testing.T) {
	pool, _, board := newTestPool(t, Options{Linger: LingerForever})
	owner := pool.NewOwner()
	lease, err := pool.Lease(Want{DeliverySystem: "ISDB-T", Owner: owner})
	if err != nil {
		t.Fatalf("take a tuner: %v", err)
	}
	lease.Release()
	if board.closed.Get() != 0 {
		t.Error("the board was closed although it was told to stay open")
	}
}

// TestCardKeepsTheBoardOpen is what the three separate counts exist for: a card
// session holds a board with no tuner running.
func TestCardKeepsTheBoardOpen(t *testing.T) {
	pool, _, board := newTestPool(t, Options{Linger: 20 * time.Millisecond})
	owner := pool.NewOwner()

	lease, err := pool.Lease(Want{DeliverySystem: "ISDB-T", Owner: owner})
	if err != nil {
		t.Fatalf("take a tuner: %v", err)
	}
	card, err := pool.Card("px4:ABC123@1-2", owner)
	if err != nil {
		t.Fatalf("take the card: %v", err)
	}
	lease.Release()

	time.Sleep(80 * time.Millisecond)
	if board.closed.Get() != 0 {
		t.Error("the board was closed while a card session still held it")
	}
	card.Release()

	deadline := time.Now().Add(2 * time.Second)
	for board.closed.Get() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the board was never closed once the card went too")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestReleaseOwnerGivesEverythingBack is what a dropped connection triggers.
func TestReleaseOwnerGivesEverythingBack(t *testing.T) {
	pool, _, _ := newTestPool(t, Options{})
	owner := pool.NewOwner()
	other := pool.NewOwner()

	first, _ := pool.Lease(Want{DeliverySystem: "ISDB-T", Owner: owner})
	_, _ = pool.Lease(Want{DeliverySystem: "ISDB-S", Owner: owner})
	kept, _ := pool.Lease(Want{DeliverySystem: "ISDB-S", Owner: other})
	if first == nil || kept == nil {
		t.Fatal("could not set the test up")
	}
	if pool.Held(owner) != 2 {
		t.Fatalf("the owner holds %d things, want 2", pool.Held(owner))
	}

	pool.ReleaseOwner(owner)

	if pool.Held(owner) != 0 {
		t.Errorf("the owner still holds %d things after its connection went", pool.Held(owner))
	}
	if pool.Held(other) != 1 {
		t.Errorf("another owner's hold was released too")
	}
	// The tuners are free again.
	if _, err := pool.Lease(Want{TunerID: first.ID(), Owner: other}); err != nil {
		t.Errorf("the released tuner could not be taken again: %v", err)
	}
}

// TestGoneEndsEverythingAtOnce covers the unplug path: streams end rather than
// hang, and nothing is retried against hardware that is gone.
func TestGoneEndsEverythingAtOnce(t *testing.T) {
	pool, registry, board := newTestPool(t, Options{})
	owner := pool.NewOwner()

	lease, err := pool.Lease(Want{DeliverySystem: "ISDB-T", Owner: owner})
	if err != nil {
		t.Fatalf("take a tuner: %v", err)
	}
	card, err := pool.Card("px4:ABC123@1-2", owner)
	if err != nil {
		t.Fatalf("take the card: %v", err)
	}

	// The board vanishes from the bus.
	registry.mutex.Lock()
	registry.detected = nil
	registry.mutex.Unlock()
	if _, err := pool.Boards(); err != nil {
		t.Fatalf("list boards after the unplug: %v", err)
	}

	select {
	case <-lease.Done():
	case <-time.After(time.Second):
		t.Error("the lease was never told its board had gone")
	}
	if !card.Stale() {
		t.Error("the card hold does not know its board has gone")
	}
	if board.closed.Get() == 0 {
		t.Error("the board handle was not given up")
	}
	if _, err := pool.Lease(Want{DeliverySystem: "ISDB-T", Owner: owner}); !errors.Is(err, ErrNoTuner) {
		t.Errorf("a tuner was handed out after the board went: %v", err)
	}
}

// TestConcurrentLeasesOpenOnce is the race the lock exists for: requests
// arriving together must not each open the hardware.
func TestConcurrentLeasesOpenOnce(t *testing.T) {
	pool, registry, _ := newTestPool(t, Options{})
	owner := pool.NewOwner()

	var wait sync.WaitGroup
	leases := make(chan *Lease, 8)
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			lease, err := pool.Lease(Want{Owner: owner})
			if err == nil {
				leases <- lease
			}
		}()
	}
	wait.Wait()
	close(leases)

	taken := 0
	for lease := range leases {
		taken++
		lease.Release()
	}
	if taken != 4 {
		t.Errorf("handed out %d tuners, want the board's 4", taken)
	}
	if got := registry.openCount(); got != 1 {
		t.Errorf("opened the board %d times under a race; it must be opened once", got)
	}
}

func TestWatchKeepsTheBoardOpenWithoutTakingATuner(t *testing.T) {
	pool, registry, _ := newTestPool(t, Options{Linger: LingerForever})
	if _, err := pool.Boards(); err != nil {
		t.Fatalf("list boards: %v", err)
	}

	stop, err := pool.Watch("px4:ABC123@1-2:0")
	if err != nil {
		t.Fatalf("watch a tuner: %v", err)
	}
	if got := registry.openCount(); got != 1 {
		t.Errorf("watching opened the board %d times, want 1", got)
	}
	// Watching does not take the tuner, so it is still free.
	owner := pool.NewOwner()
	lease, err := pool.Lease(Want{TunerID: "px4:ABC123@1-2:0", Owner: owner})
	if err != nil {
		t.Errorf("watching a tuner made it unavailable: %v", err)
	} else {
		lease.Release()
	}
	stop()
	stop() // stopping twice is harmless
}

// TestATunerThatGoesAwayWhileInUseTearsTheBoardDown: a board is usually found
// gone by trying to use it, long before anything re-reads the bus, so the handle
// must be finished from that moment.
func TestATunerThatGoesAwayWhileInUseTearsTheBoardDown(t *testing.T) {
	pool, registry, board := newTestPool(t, Options{Linger: LingerForever})
	owner := pool.NewOwner()

	lease, err := pool.Lease(Want{TunerID: "px4:ABC123@1-2:0", Owner: owner})
	if err != nil {
		t.Fatalf("lease: %v", err)
	}
	board.failTuningWith(fmt.Errorf("write register: %w", usbio.ErrGone))

	if err := lease.Device().Tune(map[string]string{"DELIVERY_SYSTEM": "ISDBS"}); !IsGone(err) {
		t.Fatalf("tuning a device that has gone said %v", err)
	}
	select {
	case <-lease.Done():
	case <-time.After(time.Second):
		t.Error("the lease was not cancelled when the board went away")
	}
	if board.closedCount() == 0 {
		t.Error("the board was not closed after its device reported it had gone")
	}

	// The next request opens again rather than reusing the failed handle.
	before := registry.openCount()
	board.failTuningWith(nil)
	if _, err := pool.Lease(Want{TunerID: "px4:ABC123@1-2:0", Owner: pool.NewOwner()}); err != nil {
		t.Fatalf("lease after the board came back: %v", err)
	}
	if registry.openCount() == before {
		t.Error("the board was reused after it had gone; it must be opened again")
	}
}

// TestATunerIsOfferedAChannelSpeltDifferently, from a MyGica T230C2 on a
// Japanese cable feed: the board advertises "DVB-C" while the channel file says
// "DVBC/ANNEX_A", the same system.
func TestATunerIsOfferedAChannelSpeltDifferently(t *testing.T) {
	board := &fakeBoard{tuners: []device.TunerInfo{
		{Index: 0, Name: "Si2141", Deliveries: []string{"DVB-T", "DVB-C", "ISDB-C"}},
	}}
	registry := &fakeRegistry{
		detected: []Detected{{
			Backend:    "t230",
			Selector:   "009:004",
			Location:   "009:004",
			Serial:     "424242",
			Deliveries: []string{"DVB-T", "DVB-C", "ISDB-C"},
			Tuners:     board.tuners,
		}},
		boards: map[string]*fakeBoard{"009:004": board},
	}
	pool := New(registry, Options{})
	t.Cleanup(func() { _ = pool.Close() })

	for _, spelling := range []string{"DVBC/ANNEX_A", "DVB-C/ANNEX_A", "DVB-C", "DVBC/ANNEX_C", "ISDB-C"} {
		lease, err := pool.Lease(Want{DeliverySystem: spelling, Owner: pool.NewOwner()})
		if err != nil {
			t.Errorf("no tuner was offered a %s channel: %v", spelling, err)
			continue
		}
		lease.Release()
	}
	// Annex B is a different system and must still be refused.
	if _, err := pool.Lease(Want{DeliverySystem: "DVBC/ANNEX_B", Owner: pool.NewOwner()}); !errors.Is(err, ErrNoTuner) {
		t.Errorf("a tuner was offered an Annex B channel it cannot receive: %v", err)
	}

	// The listing carries both spellings, since a reader matches by name.
	boards, err := pool.Boards()
	if err != nil {
		t.Fatalf("list boards: %v", err)
	}
	listed := map[string]bool{}
	for _, delivery := range boards[0].Tuners[0].Deliveries {
		listed[delivery] = true
	}
	for _, want := range []string{"DVB-C", "DVB-C/ANNEX_A", "ISDB-C", "DVB-C/ANNEX_C"} {
		if !listed[want] {
			t.Errorf("the listing does not mention %s: %v", want, boards[0].Tuners[0].Deliveries)
		}
	}
}

// TestNothingIsOpenedBeforeTheHardwareHasSettled is what lets the daemon
// answer while it is still putting right what a killed run left behind. The
// repair opens cards of its own, so a tune that walked into the middle of it
// would be two openers on one card.
func TestNothingIsOpenedBeforeTheHardwareHasSettled(t *testing.T) {
	settled := make(chan struct{})
	// The board these tests use is a px4 one, and the repair is said to be
	// that backend's.
	pool, registry, _ := newTestPool(t, Options{Settled: settled, Settling: []string{"px4"}})
	owner := pool.NewOwner()

	taken := make(chan error, 1)
	go func() {
		lease, err := pool.Lease(Want{DeliverySystem: "ISDB-T", Owner: owner})
		if lease != nil {
			lease.Release()
		}
		taken <- err
	}()

	select {
	case err := <-taken:
		t.Fatalf("the board was opened before the hardware had settled: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if got := registry.openCount(); got != 0 {
		t.Errorf("opened %d board(s) while the repair was still running", got)
	}

	close(settled)
	select {
	case err := <-taken:
		if err != nil {
			t.Fatalf("take a tuner once the hardware had settled: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the tuner was never handed over after the hardware settled")
	}
	if got := registry.openCount(); got != 1 {
		t.Errorf("opened the board %d times, want 1", got)
	}
}

// TestOnlyTheBackendBeingRepairedWaits is what keeps a card reader answering
// while a tuner of another backend is being put back. The two share nothing
// but the daemon, and a viewer whose card timed out for eleven seconds would
// be told their reader cannot be read.
func TestOnlyTheBackendBeingRepairedWaits(t *testing.T) {
	settled := make(chan struct{})
	t.Cleanup(func() { close(settled) })
	pool, registry, _ := newTestPool(t, Options{Settled: settled, Settling: []string{"tbs"}})
	owner := pool.NewOwner()

	lease, err := pool.Lease(Want{DeliverySystem: "ISDB-T", Owner: owner})
	if err != nil {
		t.Fatalf("take a tuner of a backend the repair does not touch: %v", err)
	}
	lease.Release()
	if got := registry.openCount(); got != 1 {
		t.Errorf("opened the board %d times, want 1", got)
	}
}

func TestSixUSBDevicesOnHubsLeaseIndependently(t *testing.T) {
	registry := &fakeRegistry{boards: make(map[string]*fakeBoard)}
	for hub := 1; hub <= 3; hub++ {
		for port := 1; port <= 2; port++ {
			selector := fmt.Sprintf("001:%03d.%03d", hub, port)
			board := &fakeBoard{tuners: []device.TunerInfo{{Index: 0, Name: "Test tuner", Deliveries: []string{"ISDB-T"}}}}
			registry.boards[selector] = board
			registry.detected = append(registry.detected, Detected{
				Backend: "test-usb", Selector: selector, Location: selector,
				Port: fmt.Sprintf("1-%d.%d", hub, port), Tuners: board.tuners,
				Deliveries: []string{"ISDB-T"},
			})
		}
	}
	pool := New(registry, Options{})
	defer pool.Close()
	for _, target := range registry.detected {
		id := "test-usb:p" + target.Port + ":0"
		lease, err := pool.Lease(Want{TunerID: id, Owner: pool.NewOwner()})
		if err != nil {
			t.Fatalf("lease %s: %v", id, err)
		}
		boards, err := pool.Boards()
		if err != nil {
			t.Fatal(err)
		}
		if len(boards) != 6 {
			t.Fatalf("listed %d boards, want 6", len(boards))
		}
		for _, board := range boards {
			if got, want := board.Tuners[0].Leased, board.Location == target.Selector; got != want {
				t.Errorf("leasing %s: %s leased = %v, want %v", id, board.Name, got, want)
			}
		}
		lease.Release()
	}
}

func TestSixUSBDevicesOnHubsCanStayLeasedTogether(t *testing.T) {
	registry := &fakeRegistry{boards: make(map[string]*fakeBoard)}
	for hub := 1; hub <= 3; hub++ {
		for port := 1; port <= 2; port++ {
			selector := fmt.Sprintf("001:%03d.%03d", hub, port)
			board := &fakeBoard{tuners: []device.TunerInfo{{Index: 0, Deliveries: []string{"ISDB-T"}}}}
			registry.boards[selector] = board
			registry.detected = append(registry.detected, Detected{
				Backend: "test-usb", Selector: selector, Location: selector,
				Port: fmt.Sprintf("1-%d.%d", hub, port), Tuners: board.tuners,
				Deliveries: []string{"ISDB-T"},
			})
		}
	}
	pool := New(registry, Options{})
	defer pool.Close()
	// Keep all leases alive: each request must reach a different board.
	var releases []func()
	for _, target := range registry.detected {
		lease, err := pool.Lease(Want{TunerID: "test-usb:p" + target.Port + ":0", Owner: pool.NewOwner()})
		if err != nil {
			t.Fatalf("lease %s while others are held: %v", target.Selector, err)
		}
		releases = append(releases, lease.Release)
		t.Cleanup(lease.Release)
	}
	if got := registry.openCount(); got != 6 {
		t.Fatalf("opened %d boards, want 6 independent boards", got)
	}
	releases[0]()
	boards, err := pool.Boards()
	if err != nil {
		t.Fatal(err)
	}
	for _, board := range boards {
		want := board.Location != registry.detected[0].Location
		if got := board.Tuners[0].Leased; got != want {
			t.Errorf("after releasing one device: %s leased = %v, want %v", board.Location, got, want)
		}
	}
}
