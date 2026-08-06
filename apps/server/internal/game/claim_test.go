package game

import (
	"fmt"
	"testing"
	"time"

	"github.com/jovid18/more-more/apps/server/internal/graph"
)

// 점령 판정은 state를 직접 만들어 본다. 루프를 거치면 플레이어를 원하는 자리에
// 놓을 수 없어서, 정작 확인하고 싶은 "같은 틱에 같은 곳" 상황을 만들기 어렵다.
//
// needTicks 1로 두면 매 틱이 도착이라 시나리오가 짧아진다.
func placed(t *testing.T, moves map[PlayerID]Move, order ...PlayerID) (*state, *graph.Graph) {
	t.Helper()

	g := graph.Yamanote()
	s := newState(g, 1)

	for _, id := range order {
		m, ok := moves[id]
		if !ok {
			t.Fatalf("no move given for player %d", id)
		}
		s.players[id] = &player{name: fmt.Sprintf("p%d", id), move: m}
		s.order = append(s.order, id)
	}

	return s, g
}

func edgeIndex(t *testing.T, g *graph.Graph, a, b graph.StationID) int {
	t.Helper()

	index, ok := g.EdgeIndex(a, b)
	if !ok {
		t.Fatalf("no edge between %d and %d", a, b)
	}
	return index
}

func arriving(from, to graph.StationID) Move {
	return Move{From: from, To: to, Progress: 0, NeedTicks: 1}
}

func TestFinishingAnEdgeClaimsItAndTheStationReached(t *testing.T) {
	t.Parallel()

	s, g := placed(t, map[PlayerID]Move{1: arriving(0, 1)}, 1)
	s.advance()

	if got := s.edgeOwner[edgeIndex(t, g, 0, 1)]; got != 1 {
		t.Errorf("edge 0-1 owner = %d, want 1", got)
	}
	if got := s.stationOwner[1]; got != 1 {
		t.Errorf("station 1 owner = %d, want 1", got)
	}
	// 출발한 역은 지나온 것이 아니라 서 있던 곳이다. 먹지 않는다.
	if got := s.stationOwner[0]; got != 0 {
		t.Errorf("station 0 owner = %d, want nobody", got)
	}
}

// 반대편에서 같은 역으로 동시에 들어오면 역은 아무도 못 먹는다.
// 서로 다른 구간을 지나왔으므로 구간은 각자 먹는다 — 판정이 곳마다 독립이라는 뜻이다.
func TestSimultaneousArrivalsLeaveTheStationAlone(t *testing.T) {
	t.Parallel()

	s, g := placed(t, map[PlayerID]Move{
		1: arriving(0, 1),
		2: arriving(2, 1),
	}, 1, 2)
	s.advance()

	if got := s.stationOwner[1]; got != 0 {
		t.Errorf("station 1 owner = %d, want nobody", got)
	}
	if got := s.edgeOwner[edgeIndex(t, g, 0, 1)]; got != 1 {
		t.Errorf("edge 0-1 owner = %d, want 1", got)
	}
	if got := s.edgeOwner[edgeIndex(t, g, 1, 2)]; got != 2 {
		t.Errorf("edge 1-2 owner = %d, want 2", got)
	}
}

func TestTheOwnerKeepsAStationTheyArriveAtTogetherWithAChallenger(t *testing.T) {
	t.Parallel()

	s, _ := placed(t, map[PlayerID]Move{
		1: arriving(0, 1), // 주인이 지키러 온다
		2: arriving(2, 1), // 뺏으러 온다
	}, 1, 2)
	s.stationOwner[1] = 1

	s.advance()

	if got := s.stationOwner[1]; got != 1 {
		t.Errorf("station 1 owner = %d, want the defender (1)", got)
	}
}

func TestALoneChallengerTakesAnOwnedStation(t *testing.T) {
	t.Parallel()

	s, _ := placed(t, map[PlayerID]Move{2: arriving(2, 1)}, 2)
	s.stationOwner[1] = 1

	s.advance()

	if got := s.stationOwner[1]; got != 2 {
		t.Errorf("station 1 owner = %d, want the challenger (2)", got)
	}
}

// 나란히 달리면 서로를 상쇄한다. 구간도 역도 아무도 못 먹는다.
func TestRunningTogetherClaimsNothing(t *testing.T) {
	t.Parallel()

	s, g := placed(t, map[PlayerID]Move{
		1: arriving(0, 1),
		2: arriving(0, 1),
	}, 1, 2)
	s.advance()

	if got := s.edgeOwner[edgeIndex(t, g, 0, 1)]; got != 0 {
		t.Errorf("edge 0-1 owner = %d, want nobody", got)
	}
	if got := s.stationOwner[1]; got != 0 {
		t.Errorf("station 1 owner = %d, want nobody", got)
	}
}

// 이동과 판정을 두 단계로 나눈 이유가 이것이다. 한 번에 처리하면 순회 순서가
// 빠른 쪽이 먼저 먹어, 접속 순서가 승패를 가른다.
func TestOutcomeDoesNotDependOnPlayerOrder(t *testing.T) {
	t.Parallel()

	moves := map[PlayerID]Move{
		1: arriving(0, 1),
		2: arriving(2, 1),
		3: arriving(3, 4),
	}

	first, _ := placed(t, moves, 1, 2, 3)
	first.advance()

	reversed, _ := placed(t, moves, 3, 2, 1)
	reversed.advance()

	if !slicesEqual(first.stationOwner, reversed.stationOwner) {
		t.Errorf("station owners differ by join order:\n  %v\n  %v", first.stationOwner, reversed.stationOwner)
	}
	if !slicesEqual(first.edgeOwner, reversed.edgeOwner) {
		t.Errorf("edge owners differ by join order:\n  %v\n  %v", first.edgeOwner, reversed.edgeOwner)
	}
}

func TestLeavingReleasesTheLand(t *testing.T) {
	t.Parallel()

	s, g := placed(t, map[PlayerID]Move{1: arriving(0, 1)}, 1)
	s.subs[1] = make(chan Snapshot, snapshotBuffer)
	s.advance()

	edge := edgeIndex(t, g, 0, 1)
	if s.edgeOwner[edge] != 1 || s.stationOwner[1] != 1 {
		t.Fatalf("expected player 1 to own edge %d and station 1 before leaving", edge)
	}

	s.leave(1)

	// 다시 들어오면 새 ID를 받으므로, 놓아주지 않으면 주인 없는 색만 쌓인다.
	if got := s.edgeOwner[edge]; got != 0 {
		t.Errorf("edge owner = %d after leaving, want nobody", got)
	}
	if got := s.stationOwner[1]; got != 0 {
		t.Errorf("station owner = %d after leaving, want nobody", got)
	}
}

// 스냅샷은 구독자 여럿이 나눠 갖는다. 원본을 그대로 실으면 다음 틱의 변경이
// 이미 보낸 스냅샷까지 바꿔 놓는다.
func TestSnapshotsDoNotShareOwnershipWithTheLiveState(t *testing.T) {
	t.Parallel()

	s, _ := placed(t, map[PlayerID]Move{1: arriving(0, 1)}, 1)
	taken := s.snapshot()

	s.edgeOwner[0] = 99
	s.stationOwner[0] = 99

	if taken.EdgeOwners[0] == 99 || taken.StationOwners[0] == 99 {
		t.Error("a snapshot already handed out changed when the state moved on")
	}
}

// 소유 현황이 실제로 방송까지 실려 나가는지 본다.
func TestSnapshotsCarryOwnershipToSubscribers(t *testing.T) {
	t.Parallel()

	l, ticks := startLoop(t)
	sess := mustJoin(t, l, "hana")
	recv(t, sess.Snapshots)

	for range DefaultNeedTicks {
		ticks <- time.Time{}
	}

	snap := mustSnapshot(t, l)
	if len(snap.EdgeOwners) != graph.Yamanote().EdgeCount() {
		t.Fatalf("edgeOwners = %d, want %d", len(snap.EdgeOwners), graph.Yamanote().EdgeCount())
	}

	owned := 0
	for _, owner := range snap.EdgeOwners {
		if owner == sess.ID {
			owned++
		}
	}
	if owned != 1 {
		t.Errorf("player owns %d edges after one crossing, want 1", owned)
	}
}

func slicesEqual(a, b []PlayerID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
