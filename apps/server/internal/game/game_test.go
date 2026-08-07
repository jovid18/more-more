package game

import (
	"context"
	"testing"
	"time"

	"github.com/jovid18/more-more/apps/server/internal/graph"
)

// 테스트가 실패로 판정하기까지 기다리는 시간. 넉넉히 잡는다 —
// 여기 걸린다는 것은 느린 것이 아니라 루프가 멈췄다는 뜻이다.
const waitTimeout = 2 * time.Second

// startLoop는 시간을 직접 넘기는 루프를 띄운다. time.Ticker를 쓰지 않으므로
// 테스트가 실제 시간을 기다리지 않고, 틱 경계가 정확히 어디인지도 분명해진다.
func startLoop(t *testing.T) (*Loop, chan<- time.Time) {
	t.Helper()

	l := New(graph.Yamanote())
	ticks := make(chan time.Time)
	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan struct{})
	go func() {
		defer close(done)
		l.Run(ctx, ticks)
	}()

	// 루프가 완전히 끝난 뒤에 테스트가 종료되게 한다. 그러지 않으면
	// -race가 테스트 종료와 루프 정리를 겹쳐 보고 잡음이 생긴다.
	t.Cleanup(func() {
		cancel()
		<-done
	})

	return l, ticks
}

func recv(t *testing.T, ch <-chan Snapshot) Snapshot {
	t.Helper()

	select {
	case s, ok := <-ch:
		if !ok {
			t.Fatal("snapshot channel closed unexpectedly")
		}
		return s
	case <-time.After(waitTimeout):
		t.Fatal("timed out waiting for a snapshot")
		return Snapshot{}
	}
}

func mustJoin(t *testing.T, l *Loop, name string) Session {
	t.Helper()

	sess, err := l.Join(t.Context(), name)
	if err != nil {
		t.Fatalf("Join(%q): %v", name, err)
	}
	return sess
}

func mustSnapshot(t *testing.T, l *Loop) Snapshot {
	t.Helper()

	snap, err := l.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	return snap
}

func onlyPlayer(t *testing.T, snap Snapshot) PlayerState {
	t.Helper()

	if len(snap.Players) != 1 {
		t.Fatalf("players = %d, want 1", len(snap.Players))
	}
	return snap.Players[0]
}

func TestJoinSendsTheCurrentStateImmediately(t *testing.T) {
	t.Parallel()

	l, _ := startLoop(t)
	sess := mustJoin(t, l, "hana")

	// 접속하자마자 한 장 와야 한다. 안 그러면 첫 틱까지 화면이 비어 있다.
	snap := recv(t, sess.Snapshots)
	if snap.Tick != 0 {
		t.Errorf("tick = %d, want 0", snap.Tick)
	}

	p := onlyPlayer(t, snap)
	if p.ID != sess.ID {
		t.Errorf("id = %d, want %d", p.ID, sess.ID)
	}
	if p.Name != "hana" {
		t.Errorf("name = %q, want %q", p.Name, "hana")
	}
	if p.Move.Progress != 0 || p.Move.TotalTicks != graph.YamanoteTravelTicks {
		t.Errorf("move = %+v, want progress 0 and needTicks %d", p.Move, graph.YamanoteTravelTicks)
	}
	if p.Move.From == p.Move.To {
		t.Errorf("move starts and ends at the same station %d", p.Move.From)
	}
}

func TestEachTickAdvancesProgressByOne(t *testing.T) {
	t.Parallel()

	l, ticks := startLoop(t)
	sess := mustJoin(t, l, "hana")
	start := onlyPlayer(t, recv(t, sess.Snapshots)).Move

	for i := 1; i < graph.YamanoteTravelTicks; i++ {
		ticks <- time.Time{}

		snap := recv(t, sess.Snapshots)
		if snap.Tick != uint64(i) {
			t.Fatalf("tick = %d, want %d", snap.Tick, i)
		}

		move := onlyPlayer(t, snap).Move
		if move.Progress != i {
			t.Fatalf("after %d ticks progress = %d, want %d", i, move.Progress, i)
		}
		if move.From != start.From || move.To != start.To {
			t.Fatalf("moved to %d→%d before arriving, want %d→%d", move.From, move.To, start.From, start.To)
		}
	}
}

// TotalTicks번째 틱에 도착한다. 역에 머무는 상태는 없으므로 그 자리에서
// 곧바로 다음 구간에 오르고, progress는 0으로 돌아간다.
func TestArrivalMovesOntoTheNextEdge(t *testing.T) {
	t.Parallel()

	l, ticks := startLoop(t)
	sess := mustJoin(t, l, "hana")
	start := onlyPlayer(t, recv(t, sess.Snapshots)).Move

	for range graph.YamanoteTravelTicks {
		ticks <- time.Time{}
	}

	move := onlyPlayer(t, mustSnapshot(t, l)).Move
	if move.From != start.To {
		t.Errorf("from = %d, want %d (the station just reached)", move.From, start.To)
	}
	if move.To == start.From {
		t.Errorf("turned back to %d instead of carrying on", move.To)
	}
	if move.Progress != 0 {
		t.Errorf("progress = %d, want 0", move.Progress)
	}
}

// 야마노테선은 30역 순환이므로 30구간을 지나면 출발점으로 돌아온다.
// 그래프와 이동이 함께 맞아야만 성립한다.
func TestOneFullLapReturnsToTheStart(t *testing.T) {
	t.Parallel()

	l, ticks := startLoop(t)
	sess := mustJoin(t, l, "hana")
	start := onlyPlayer(t, recv(t, sess.Snapshots)).Move

	stations := graph.Yamanote().Len()
	for range stations * graph.YamanoteTravelTicks {
		ticks <- time.Time{}
	}

	if move := onlyPlayer(t, mustSnapshot(t, l)).Move; move != start {
		t.Errorf("after one lap move = %+v, want %+v", move, start)
	}
}

func TestReverseMirrorsProgressWithinTheEdge(t *testing.T) {
	t.Parallel()

	l, ticks := startLoop(t)
	sess := mustJoin(t, l, "hana")
	start := onlyPlayer(t, recv(t, sess.Snapshots)).Move

	ticks <- time.Time{} // progress 1/3
	recv(t, sess.Snapshots)

	if err := l.Reverse(t.Context(), sess.ID); err != nil {
		t.Fatalf("Reverse: %v", err)
	}

	// 명령 채널은 FIFO다. Reverse 다음에 요청한 스냅샷에는 반드시 반영돼 있다.
	move := onlyPlayer(t, mustSnapshot(t, l)).Move

	want := Move{From: start.To, To: start.From, Progress: graph.YamanoteTravelTicks - 1, TotalTicks: graph.YamanoteTravelTicks}
	if move != want {
		t.Errorf("move = %+v, want %+v", move, want)
	}
}

func TestLeaveClosesTheChannelButKeepsTheSeat(t *testing.T) {
	t.Parallel()

	l, _ := startLoop(t)
	staying := mustJoin(t, l, "staying")
	leaving := mustJoin(t, l, "leaving")
	recv(t, staying.Snapshots)
	recv(t, leaving.Snapshots)

	if err := l.Leave(t.Context(), leaving.ID); err != nil {
		t.Fatalf("Leave: %v", err)
	}

	// 채널이 닫히는 것이 연결 쪽 쓰기 goroutine에게 나가라는 신호다.
	select {
	case _, ok := <-leaving.Snapshots:
		if ok {
			t.Error("received a snapshot after leaving")
		}
	case <-time.After(waitTimeout):
		t.Fatal("the channel was not closed after Leave")
	}

	// 플레이어 자체는 남는다. 유예 동안 돌아올 자리를 붙들어 두기 때문이다.
	snap := mustSnapshot(t, l)
	if len(snap.Players) != 2 {
		t.Fatalf("players = %d, want 2 (one of them away)", len(snap.Players))
	}
	for _, p := range snap.Players {
		if want := p.ID == leaving.ID; p.Away != want {
			t.Errorf("player %d away = %v, want %v", p.ID, p.Away, want)
		}
	}
}

// 이 프로젝트에서 가장 중요한 테스트다.
//
// 스냅샷을 전혀 읽지 않는 연결이 하나 있어도 틱은 계속 돌아야 한다.
// 방송이 블로킹이었다면 여기서 ticks 전송이 막혀 타임아웃으로 죽는다.
func TestASilentSubscriberDoesNotStallTheLoop(t *testing.T) {
	t.Parallel()

	l, ticks := startLoop(t)
	silent := mustJoin(t, l, "silent") // 일부러 한 번도 읽지 않는다
	active := mustJoin(t, l, "active")
	recv(t, active.Snapshots)

	const rounds = 20
	for i := 1; i <= rounds; i++ {
		select {
		case ticks <- time.Time{}:
		case <-time.After(waitTimeout):
			t.Fatalf("tick %d blocked: a slow subscriber stalled the loop", i)
		}
		recv(t, active.Snapshots)
	}

	// 조용한 쪽도 살아 있고, 밀린 것이 아니라 최신을 들고 있어야 한다.
	// 버퍼가 1이므로 그 안의 한 장은 가장 최근 틱이다.
	snap := recv(t, silent.Snapshots)
	if snap.Tick != rounds {
		t.Errorf("the silent subscriber holds tick %d, want the newest (%d)", snap.Tick, rounds)
	}
}

func TestRunClosesEverySubscriberWhenTheContextEnds(t *testing.T) {
	t.Parallel()

	l := New(graph.Yamanote())
	ticks := make(chan time.Time)
	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan struct{})
	go func() {
		defer close(done)
		l.Run(ctx, ticks)
	}()

	sess := mustJoin(t, l, "hana")
	recv(t, sess.Snapshots)

	cancel()

	select {
	case <-done:
	case <-time.After(waitTimeout):
		t.Fatal("Run did not return after the context was cancelled")
	}

	// 버퍼에 남은 것을 다 읽고 나면 닫혀 있어야 한다.
	for {
		select {
		case _, ok := <-sess.Snapshots:
			if !ok {
				return
			}
		case <-time.After(waitTimeout):
			t.Fatal("the subscriber channel was left open after shutdown")
		}
	}
}

func TestCommandsFailOnceTheCallerContextIsDone(t *testing.T) {
	t.Parallel()

	// 일부러 Run을 띄우지 않는다. 명령 채널을 비우는 쪽이 없으므로
	// 버퍼가 찬 뒤에는 ctx만이 호출자를 풀어줄 수 있다.
	l := New(graph.Yamanote())

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := l.Join(ctx, "hana"); err == nil {
		t.Error("Join succeeded with a cancelled context, want an error")
	}
}
