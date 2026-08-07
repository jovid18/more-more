package hub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/jovid18/more-more/apps/server/internal/game"
	"github.com/jovid18/more-more/apps/server/internal/graph"
)

const (
	waitTimeout  = 2 * time.Second
	testTickRate = 500 * time.Millisecond
)

// message는 welcome과 snapshot을 한 타입으로 받는다. 실제 클라이언트도
// type을 보고 갈라 읽으므로, 테스트가 그 형태를 그대로 흉내낸다.
type message struct {
	Type string `json:"type"`

	You      game.PlayerID   `json:"you"`
	TickMS   int64           `json:"tickMs"`
	Stations []graph.Station `json:"stations"`
	Edges    []graph.Edge    `json:"edges"`

	Tick          uint64             `json:"tick"`
	Players       []game.PlayerState `json:"players"`
	EdgeOwners    []game.PlayerID    `json:"edgeOwners"`
	StationOwners []game.PlayerID    `json:"stationOwners"`
}

type harness struct {
	url     string
	ticks   chan<- time.Time
	loop    *game.Loop
	stop    func()
	stopped chan struct{}
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	g := graph.Yamanote()
	loop := game.New(g)
	ticks := make(chan time.Time)

	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		loop.Run(ctx, ticks)
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws", New(loop, g, testTickRate).Handler(ctx))
	srv := httptest.NewServer(mux)

	var once bool
	stop := func() {
		if once {
			return
		}
		once = true
		// 반드시 취소가 먼저다. 핸들러는 ctx가 살아 있는 동안 반환하지 않으므로,
		// srv.Close()를 먼저 부르면 그 안에서 서로를 기다리며 멈춘다.
		cancel()
		<-stopped
		srv.Close()
	}
	t.Cleanup(stop)

	return &harness{
		url:     "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws",
		ticks:   ticks,
		loop:    loop,
		stop:    stop,
		stopped: stopped,
	}
}

func (h *harness) dial(t *testing.T, name string) *websocket.Conn {
	t.Helper()

	conn, _, err := websocket.Dial(t.Context(), h.url+"?name="+name, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

func read(t *testing.T, conn *websocket.Conn) message {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), waitTimeout)
	defer cancel()

	var msg message
	if err := wsjson.Read(ctx, conn, &msg); err != nil {
		t.Fatalf("read: %v", err)
	}
	return msg
}

func readSnapshot(t *testing.T, conn *websocket.Conn) message {
	t.Helper()

	msg := read(t, conn)
	if msg.Type != "snapshot" {
		t.Fatalf("type = %q, want %q", msg.Type, "snapshot")
	}
	return msg
}

// 클라이언트가 보낸 명령이 루프에 닿는 것은 비동기다. 눈에 보일 때까지 기다린다.
func (h *harness) waitFor(t *testing.T, want func(game.Snapshot) bool) game.Snapshot {
	t.Helper()

	deadline := time.Now().Add(waitTimeout)
	for time.Now().Before(deadline) {
		snap, err := h.loop.Snapshot(t.Context())
		if err != nil {
			t.Fatalf("Snapshot: %v", err)
		}
		if want(snap) {
			return snap
		}
		time.Sleep(5 * time.Millisecond)
	}

	t.Fatal("timed out waiting for the loop to reach the expected state")
	return game.Snapshot{}
}

func TestWelcomeCarriesTheMapOnce(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	msg := read(t, h.dial(t, "hana"))

	if msg.Type != "welcome" {
		t.Fatalf("type = %q, want %q", msg.Type, "welcome")
	}
	if msg.You == 0 {
		t.Error("you = 0, want an assigned player id")
	}
	if got := len(msg.Stations); got != 30 {
		t.Errorf("stations = %d, want 30", got)
	}
	if got := len(msg.Edges); got != 30 {
		t.Errorf("edges = %d, want 30", got)
	}
	if msg.TickMS != testTickRate.Milliseconds() {
		t.Errorf("tickMs = %d, want %d", msg.TickMS, testTickRate.Milliseconds())
	}

	// 역 이름이 실려야 클라이언트가 지도를 그린다.
	if msg.Stations[0].Name == "" || msg.Stations[0].Romaji == "" {
		t.Errorf("station 0 = %+v, want both names filled in", msg.Stations[0])
	}
}

func TestEveryTickSendsASnapshot(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	conn := h.dial(t, "hana")
	read(t, conn) // welcome

	// 접속 직후 한 장. 첫 틱까지 화면이 비지 않게 하는 것이다.
	first := readSnapshot(t, conn)
	if first.Tick != 0 || len(first.Players) != 1 {
		t.Fatalf("first snapshot = tick %d with %d players, want tick 0 with 1", first.Tick, len(first.Players))
	}
	start := first.Players[0].Move

	for i := 1; i <= graph.YamanoteTravelTicks; i++ {
		h.ticks <- time.Time{}

		msg := readSnapshot(t, conn)
		if msg.Tick != uint64(i) {
			t.Fatalf("tick = %d, want %d", msg.Tick, i)
		}

		move := msg.Players[0].Move
		switch {
		case i < graph.YamanoteTravelTicks:
			if move.Progress != i {
				t.Errorf("tick %d: progress = %d, want %d", i, move.Progress, i)
			}
		default:
			// 마지막 틱에 도착해 다음 구간으로 넘어간다.
			if move.From != start.To || move.Progress != 0 {
				t.Errorf("tick %d: move = %+v, want a fresh edge from %d", i, move, start.To)
			}
		}
	}
}

func TestReverseFromTheClientTurnsThePlayerAround(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	conn := h.dial(t, "hana")
	welcome := read(t, conn)
	start := readSnapshot(t, conn).Players[0].Move

	ctx, cancel := context.WithTimeout(t.Context(), waitTimeout)
	defer cancel()
	if err := wsjson.Write(ctx, conn, clientMessage{Type: "reverse"}); err != nil {
		t.Fatalf("write: %v", err)
	}

	snap := h.waitFor(t, func(s game.Snapshot) bool {
		return len(s.Players) == 1 && s.Players[0].Move.From == start.To
	})

	move := snap.Players[0].Move
	if move.To != start.From {
		t.Errorf("move = %+v, want it heading back to %d", move, start.From)
	}
	if snap.Players[0].ID != welcome.You {
		t.Errorf("player id = %d, want %d", snap.Players[0].ID, welcome.You)
	}
}

func TestUnknownMessagesAreIgnored(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	conn := h.dial(t, "hana")
	read(t, conn)
	readSnapshot(t, conn)

	ctx, cancel := context.WithTimeout(t.Context(), waitTimeout)
	defer cancel()
	if err := wsjson.Write(ctx, conn, clientMessage{Type: "definitely-not-a-command"}); err != nil {
		t.Fatalf("write: %v", err)
	}

	// 연결이 살아 있어야 한다. 모르는 명령 하나로 접속이 끊기면
	// 클라이언트 버전이 갈릴 때마다 게임이 끊긴다.
	h.ticks <- time.Time{}
	if msg := readSnapshot(t, conn); msg.Tick != 1 {
		t.Errorf("tick = %d, want 1", msg.Tick)
	}
}

func TestLeavingTheGameClosesTheSocket(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	conn := h.dial(t, "hana")
	welcome := read(t, conn)
	readSnapshot(t, conn)

	if err := h.loop.Leave(t.Context(), welcome.You); err != nil {
		t.Fatalf("Leave: %v", err)
	}

	// 구독 채널이 닫히면 쓰기 goroutine이 소켓을 정상 종료로 닫는다.
	ctx, cancel := context.WithTimeout(t.Context(), waitTimeout)
	defer cancel()

	var msg message
	err := wsjson.Read(ctx, conn, &msg)
	if got := websocket.CloseStatus(err); got != websocket.StatusNormalClosure {
		t.Errorf("close status = %v (err %v), want %v", got, err, websocket.StatusNormalClosure)
	}
}

func TestDisconnectingRemovesThePlayer(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	conn := h.dial(t, "hana")
	read(t, conn)
	readSnapshot(t, conn)

	if err := conn.Close(websocket.StatusNormalClosure, "bye"); err != nil {
		t.Fatalf("close: %v", err)
	}

	// 연결이 끊기면 읽기 goroutine이 죽고, 그 defer가 Leave를 보낸다.
	// 이게 동작하지 않으면 유령 플레이어가 지도에 남는다.
	h.waitFor(t, func(s game.Snapshot) bool { return len(s.Players) == 0 })
}

func TestShuttingDownClosesOpenConnections(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	conn := h.dial(t, "hana")
	read(t, conn)
	readSnapshot(t, conn)

	// 서버가 내려갈 때 열린 게임 접속이 붙잡고 있으면 graceful shutdown이
	// 타임아웃까지 늘어진다. stop()이 바로 반환하는 것 자체가 검증이다.
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.stop()
	}()

	select {
	case <-done:
	case <-time.After(waitTimeout):
		t.Fatal("shutdown blocked on an open websocket connection")
	}
}
