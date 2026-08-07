package game

import (
	"testing"
	"time"

	"github.com/jovid18/more-more/apps/server/internal/graph"
)

// 야마노테선 기준 값. 전 역의 차수가 2이고 전 구간이 3틱이므로 균일하다.
var (
	stationPoints = 2 * DefaultScoring.PerStationDegree
	edgePoints    = graph.YamanoteTravelTicks * DefaultScoring.PerEdgeTick
)

// 구간을 하나 건너면 구간과 역을 하나씩 먹는다. 점수는 둘의 합이다.
func TestScoreCountsOwnedEdgesAndStations(t *testing.T) {
	t.Parallel()

	s, _ := placed(t, map[PlayerID]Move{1: arriving(0, 1)}, 1)
	s.advance()

	if got, want := s.scores()[1], edgePoints+stationPoints; got != want {
		t.Errorf("score = %d, want %d (구간 %d + 역 %d)", got, want, edgePoints, stationPoints)
	}
}

// 가중치가 실제로 곱해지는지 본다. 구간과 역을 같은 값으로 두면 어느 쪽이
// 세어졌는지 구분되지 않으므로 일부러 다르게 준다.
func TestScoreAppliesTheWeights(t *testing.T) {
	t.Parallel()

	g := graph.Yamanote()
	s := newState(g, Scoring{PerStationDegree: 100, PerEdgeTick: 1})

	edge, ok := g.EdgeIndex(0, 1)
	if !ok {
		t.Fatal("no edge 0-1")
	}
	s.edgeOwner[edge] = 7
	s.stationOwner[1] = 7

	// 역: 차수 2 × 100 = 200, 구간: 3틱 × 1 = 3
	if got, want := s.scores()[7], 203; got != want {
		t.Errorf("score = %d, want %d", got, want)
	}
}

// 주인 없는 땅은 아무에게도 점수를 주지 않는다. 0을 플레이어로 세면
// 유령 플레이어가 순위표에 나타난다.
func TestUnownedLandScoresForNobody(t *testing.T) {
	t.Parallel()

	s, _ := placed(t, map[PlayerID]Move{})
	if len(s.scores()) != 0 {
		t.Errorf("scores = %v, want empty", s.scores())
	}
}

func TestRoundEndsAfterRoundTicks(t *testing.T) {
	t.Parallel()

	s, _ := placed(t, map[PlayerID]Move{1: arriving(0, 1)}, 1)

	for range RoundTicks - 1 {
		s.advance()
	}
	if s.phase != PhasePlaying {
		t.Fatalf("phase = %q one tick early, want %q", s.phase, PhasePlaying)
	}

	s.advance()
	if s.phase != PhaseFinished {
		t.Errorf("phase = %q, want %q", s.phase, PhaseFinished)
	}
	if s.winner != 1 {
		t.Errorf("winner = %d, want 1 (the only player owning anything)", s.winner)
	}
}

// 마지막 틱에 뺏은 땅도 점수에 들어가야 한다. 판정을 점령보다 앞에 두면
// 종료 직전의 한 수가 통째로 사라진다.
func TestTheFinalTickStillClaims(t *testing.T) {
	t.Parallel()

	s, g := placed(t, map[PlayerID]Move{1: arriving(0, 1)}, 1)

	// 마지막 한 틱을 남겨두고, 그 틱에 도착하도록 맞춘다.
	for range RoundTicks - 1 {
		s.advance()
	}
	edge := edgeIndex(t, g, 0, 1)
	s.edgeOwner[edge] = 0
	s.stationOwner[1] = 0
	s.players[1].move = arriving(0, 1)

	s.advance()

	if got := s.edgeOwner[edge]; got != 1 {
		t.Errorf("edge owner after the final tick = %d, want 1", got)
	}
	if s.winner != 1 {
		t.Errorf("winner = %d, want 1", s.winner)
	}
}

func TestATiedRoundHasNoWinner(t *testing.T) {
	t.Parallel()

	s, g := placed(t, map[PlayerID]Move{})
	// 서로 다른 구간을 하나씩. 구간 값이 균일하므로 점수가 같다.
	s.edgeOwner[edgeIndex(t, g, 0, 1)] = 1
	s.edgeOwner[edgeIndex(t, g, 5, 6)] = 2

	s.finishRound()

	if s.winner != 0 {
		t.Errorf("winner = %d, want 0 for a tie", s.winner)
	}
}

func TestAnEmptyRoundHasNoWinner(t *testing.T) {
	t.Parallel()

	s, _ := placed(t, map[PlayerID]Move{})
	s.finishRound()

	if s.winner != 0 {
		t.Errorf("winner = %d, want 0 when nobody owns anything", s.winner)
	}
}

// 결과를 보여주는 동안에는 아무도 움직이지 않는다. 지도가 그대로 남아야
// 누가 어떻게 이겼는지 볼 수 있다.
func TestNobodyMovesWhileTheResultIsShown(t *testing.T) {
	t.Parallel()

	s, _ := placed(t, map[PlayerID]Move{1: {From: 0, To: 1, TotalTicks: 10}}, 1)
	s.finishRound()

	before := s.players[1].move
	s.advance()

	if s.players[1].move != before {
		t.Errorf("move = %+v during the result screen, want %+v", s.players[1].move, before)
	}
}

func TestTheNextRoundClearsTheMap(t *testing.T) {
	t.Parallel()

	s, g := placed(t, map[PlayerID]Move{1: arriving(0, 1)}, 1)
	s.advance()
	if s.scores()[1] == 0 {
		t.Fatal("expected player 1 to own something before the round ends")
	}

	s.finishRound()
	for range FinishedTicks {
		s.advance()
	}

	if s.phase != PhasePlaying {
		t.Errorf("phase = %q, want %q", s.phase, PhasePlaying)
	}
	if s.winner != 0 {
		t.Errorf("winner = %d, want 0 once the next round starts", s.winner)
	}
	if got := len(s.scores()); got != 0 {
		t.Errorf("%d players still hold land, want the map cleared", got)
	}
	if got := s.edgeOwner[edgeIndex(t, g, 0, 1)]; got != 0 {
		t.Errorf("edge owner = %d, want nobody", got)
	}
}

// 판이 바뀌어도 플레이어는 있던 자리에 그대로 있어야 한다. 위치까지 되돌리면
// 화면에서 전원이 순간이동하고, 클라이언트 보간이 그 한 틱을 직선으로 잇는다.
func TestTheNextRoundLeavesPlayersWhereTheyAre(t *testing.T) {
	t.Parallel()

	s, _ := placed(t, map[PlayerID]Move{1: {From: 4, To: 5, Progress: 1, TotalTicks: 3}}, 1)
	s.finishRound()

	before := s.players[1].move
	for range FinishedTicks {
		s.advance()
	}

	if s.players[1].move != before {
		t.Errorf("move = %+v after the reset, want %+v", s.players[1].move, before)
	}
}

func TestTicksLeftCountsDownInBothPhases(t *testing.T) {
	t.Parallel()

	s, _ := placed(t, map[PlayerID]Move{})

	if got := s.ticksLeft(); got != RoundTicks {
		t.Errorf("ticksLeft at the start = %d, want %d", got, RoundTicks)
	}

	s.advance()
	if got := s.ticksLeft(); got != RoundTicks-1 {
		t.Errorf("ticksLeft after one tick = %d, want %d", got, RoundTicks-1)
	}

	s.finishRound()
	if got := s.ticksLeft(); got != FinishedTicks {
		t.Errorf("ticksLeft once finished = %d, want %d", got, FinishedTicks)
	}
}

// 스냅샷에 실제로 실려 나가는지 본다.
func TestSnapshotsCarryScoreAndPhase(t *testing.T) {
	t.Parallel()

	l, ticks := startLoop(t)
	sess := mustJoin(t, l, "hana")

	first := recv(t, sess.Snapshots)
	if first.Phase != PhasePlaying {
		t.Errorf("phase = %q, want %q", first.Phase, PhasePlaying)
	}
	if first.TicksLeft != RoundTicks {
		t.Errorf("ticksLeft = %d, want %d", first.TicksLeft, RoundTicks)
	}
	if got := first.Players[0].Score; got != 0 {
		t.Errorf("score before moving = %d, want 0", got)
	}

	for range graph.YamanoteTravelTicks {
		ticks <- time.Time{}
	}

	snap := mustSnapshot(t, l)
	if got, want := snap.Players[0].Score, edgePoints+stationPoints; got != want {
		t.Errorf("score after one crossing = %d, want %d", got, want)
	}
	if snap.Winner != 0 {
		t.Errorf("winner = %d mid-round, want 0", snap.Winner)
	}
}
