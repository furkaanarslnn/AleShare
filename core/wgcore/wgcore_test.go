package wgcore

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"runtime"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hotspot/core/internal/pairing"
)

func privateKeyOf(conf string) string {
	for _, line := range strings.Split(conf, "\n") {
		if v, ok := strings.CutPrefix(line, "PrivateKey = "); ok {
			return v
		}
	}
	return ""
}

func TestProvisioning(t *testing.T) {
	c, err := NewCore("")
	if err != nil {
		t.Fatal(err)
	}

	laptop, err := c.AddPeer("laptop", "192.168.49.1:51820")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Endpoint = 192.168.49.1:51820", "Address = 10.8.0.2/32", "DNS = 10.8.0.1", "AllowedIPs = 0.0.0.0/0, ::/0"} {
		if !strings.Contains(laptop, want) {
			t.Fatalf("config lacks %q:\n%s", want, laptop)
		}
	}
	phone, err := c.AddPeer("phone", "192.168.49.1:51820")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(phone, "Address = 10.8.0.3/32") {
		t.Fatalf("second client got the wrong address:\n%s", phone)
	}
	if _, err := c.AddPeer("phone", "192.168.49.1:51820"); err == nil {
		t.Fatal("duplicate client name accepted")
	}
	if _, err := c.AddPeer("bad", "not-an-endpoint"); err == nil {
		t.Fatal("bad endpoint accepted")
	}

	if !c.RemovePeer("laptop") || c.RemovePeer("laptop") {
		t.Fatal("RemovePeer results wrong")
	}
	tablet, err := c.AddPeer("tablet", "192.168.49.1:51820")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tablet, "Address = 10.8.0.2/32") {
		t.Fatalf("freed address was not reused:\n%s", tablet)
	}

	js, err := c.StateJSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, conf := range []string{phone, tablet} {
		if key := privateKeyOf(conf); key == "" || strings.Contains(js, key) {
			t.Fatal("a client private key is missing from its config or leaked into the state")
		}
	}

	c2, err := NewCore(js)
	if err != nil {
		t.Fatal(err)
	}
	if got := c2.PeerNames(); got != "phone\ntablet" {
		t.Fatalf("restored clients = %q", got)
	}
}

func freeUDPPort(t *testing.T) int {
	t.Helper()
	l, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.LocalAddr().(*net.UDPAddr).Port
}

func TestStartStop(t *testing.T) {
	c, err := NewCore("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddPeer("phone", "127.0.0.1:51820"); err != nil {
		t.Fatal(err)
	}
	port := freeUDPPort(t)
	if err := c.Start(port, nil); err != nil {
		t.Fatal(err)
	}
	if !c.Running() {
		t.Fatal("not running after Start")
	}
	if err := c.Start(port, nil); err == nil {
		t.Fatal("second Start accepted")
	}
	// Adding a client while running must reach the live server.
	if _, err := c.AddPeer("laptop", "127.0.0.1:51820"); err != nil {
		t.Fatal(err)
	}
	if got := c.Stats(); got.TCPTotal != 0 {
		t.Fatalf("fresh core has traffic: %+v", got)
	}
	c.Stop()
	if c.Running() {
		t.Fatal("still running after Stop")
	}
	if err := c.Start(port, nil); err != nil {
		t.Fatalf("restart: %v", err)
	}
	c.Stop()
	if err := c.Start(0, nil); err == nil {
		t.Fatal("port 0 accepted")
	}
}

type fakeListener struct {
	approve bool
	sas     chan string
	paired  chan string
}

func (f *fakeListener) Approve(name, sas string) bool {
	f.sas <- sas
	return f.approve
}

func (f *fakeListener) Paired(name string) { f.paired <- name }

func TestPairingEnrollsAComputer(t *testing.T) {
	c, err := NewCore("")
	if err != nil {
		t.Fatal(err)
	}
	// The gateway runs: enrolling must reach the live server.
	if err := c.Start(freeUDPPort(t), nil); err != nil {
		t.Fatal(err)
	}
	defer c.Stop()

	l := &fakeListener{approve: true, sas: make(chan string, 1), paired: make(chan string, 1)}
	if err := c.startPairing("127.0.0.1:0", l); err != nil {
		t.Fatal(err)
	}
	if !c.Pairing() {
		t.Fatal("pairing window not open")
	}
	if err := c.startPairing("127.0.0.1:0", l); err == nil {
		t.Fatal("a second window was opened")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var shown string
	res, err := pairing.Pair(ctx, c.pairing.Addr(), "DESKTOP-TEST", func(sas string) { shown = sas })
	if err != nil {
		t.Fatal(err)
	}
	if got := <-l.sas; got != shown {
		t.Fatalf("numbers differ: phone %q, computer %q", got, shown)
	}
	if got := <-l.paired; got != "DESKTOP-TEST" {
		t.Fatalf("Paired(%q)", got)
	}
	if res.Grant.IPv4 != netip.MustParseAddr("10.8.0.2") || res.Grant.Port != c.wgPort {
		t.Fatalf("grant = %+v", res.Grant)
	}
	if c.PeerNames() != "DESKTOP-TEST" {
		t.Fatalf("peers = %q", c.PeerNames())
	}
	js, _ := c.StateJSON()
	if !strings.Contains(js, res.Private.Public().Base64()) || strings.Contains(js, res.Private.Base64()) {
		t.Fatal("the state must hold the computer's public key and never its private key")
	}
	// One pairing per window.
	deadline := time.Now().Add(5 * time.Second)
	for c.Pairing() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if c.Pairing() {
		t.Fatal("the window stayed open after a pairing")
	}
}

func TestPairingRefusedLeavesNoTrace(t *testing.T) {
	c, _ := NewCore("")
	l := &fakeListener{approve: false, sas: make(chan string, 1), paired: make(chan string, 1)}
	if err := c.startPairing("127.0.0.1:0", l); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := pairing.Pair(ctx, c.pairing.Addr(), "intruder", func(string) {}); !errors.Is(err, pairing.ErrDenied) {
		t.Fatalf("err = %v", err)
	}
	if c.PeerNames() != "" {
		t.Fatalf("peers = %q", c.PeerNames())
	}
	select {
	case n := <-l.paired:
		t.Fatalf("Paired(%q) after a refusal", n)
	default:
	}
}

// A computer code scanned while no window was open (the scanner closes the screen's window) is admitted through
// the next window, without the user being asked, and only once.
func TestScannedComputerJoinsThroughTheNextWindow(t *testing.T) {
	c, _ := NewCore("")
	priv, code, err := pairing.NewQR()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ExpectComputer("not a code"); err == nil {
		t.Fatal("took a text that is not a computer's code")
	}
	if err := c.ExpectComputer(code.String()); err != nil {
		t.Fatal(err)
	}
	l := &fakeListener{approve: false, sas: make(chan string, 1), paired: make(chan string, 1)}
	if err := c.startPairing("127.0.0.1:0", l); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := pairing.PairQR(ctx, c.pairing.Addr(), "LAPTOP", priv, code); err != nil {
		t.Fatal(err)
	}
	if got := <-l.paired; got != "LAPTOP" || c.PeerNames() != "LAPTOP" {
		t.Fatalf("Paired(%q), peers %q", got, c.PeerNames())
	}
	select {
	case <-l.sas:
		t.Fatal("the user was asked about a scanned computer")
	default:
	}
	c.mu.Lock()
	left := c.expectedLocked()
	c.mu.Unlock()
	if left != nil {
		t.Fatal("the code is still good after its computer paired")
	}
}

func TestRuntimeIsTunedOnlyWhileRunning(t *testing.T) {
	before := runtime.GOMAXPROCS(0)
	gcBefore := debug.SetGCPercent(-1) // reads the setting (and pauses GC for an instant)
	debug.SetGCPercent(gcBefore)

	c, err := NewCore("")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(freeUDPPort(t), nil); err != nil {
		t.Fatal(err)
	}
	want := tunedProcs
	if runtime.NumCPU() < want {
		want = runtime.NumCPU()
	}
	if got := runtime.GOMAXPROCS(0); got != want {
		t.Fatalf("GOMAXPROCS while running = %d, want %d", got, want)
	}
	c.Stop()
	if got := runtime.GOMAXPROCS(0); got != before {
		t.Fatalf("GOMAXPROCS after Stop = %d, want %d (restored)", got, before)
	}
	if got := debug.SetGCPercent(gcBefore); got != gcBefore {
		t.Fatalf("GC percent after Stop = %d, want %d (restored)", got, gcBefore)
	}
}

func TestSetTuningAppliesWhileRunningAndIsUndone(t *testing.T) {
	before := runtime.GOMAXPROCS(0)
	c, _ := NewCore("")
	if err := c.Start(freeUDPPort(t), nil); err != nil {
		t.Fatal(err)
	}
	c.SetTuning(1, 200)
	if got := runtime.GOMAXPROCS(0); got != 1 {
		t.Fatalf("GOMAXPROCS = %d, want 1", got)
	}
	c.Stop()
	if got := runtime.GOMAXPROCS(0); got != before {
		t.Fatalf("GOMAXPROCS after Stop = %d, want %d", got, before)
	}
}

func TestCPUProfileWritesAFile(t *testing.T) {
	c, _ := NewCore("")
	path := t.TempDir() + "/cpu.prof"
	if err := c.StartCPUProfile(path); err != nil {
		t.Fatal(err)
	}
	if err := c.StartCPUProfile(path); err == nil {
		t.Fatal("a second profile was started")
	}
	time.Sleep(100 * time.Millisecond)
	c.StopCPUProfile()
	c.StopCPUProfile() // harmless
}

type fakeProtector struct {
	calls atomic.Int32
	ok    bool
}

func (f *fakeProtector) Protect(fd int) bool {
	f.calls.Add(1)
	return f.ok
}

func TestProtectedDialer(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	allow := &fakeProtector{ok: true}
	conn, err := protectedDialer{allow}.DialContext(ctx, "tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if allow.calls.Load() != 1 {
		t.Fatalf("Protect called %d times, want once per socket", allow.calls.Load())
	}

	deny := &fakeProtector{ok: false}
	if conn, err := (protectedDialer{deny}).DialContext(ctx, "tcp", l.Addr().String()); err == nil {
		conn.Close()
		t.Fatal("dial succeeded although the socket was not protected")
	}
}
