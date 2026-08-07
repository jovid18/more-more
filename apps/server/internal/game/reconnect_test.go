package game

import (
	"testing"
)

// 끊긴 뒤에도 자리와 땅은 유예가 끝날 때까지 남는다.
func TestLeavingHoldsTheSeatForTheGracePeriod(t *testing.T) {
	t.Parallel()

	s, g := placed(t, map[PlayerID]Move{1: arriving(0, 1)}, 1)
	s.subs[1] = make(chan Snapshot, snapshotBuffer)
	s.advance()

	edge := edgeIndex(t, g, 0, 1)
	if s.edgeOwner[edge] != 1 || s.stationOwner[1] != 1 {
		t.Fatalf("expected player 1 to own edge %d and station 1 before leaving", edge)
	}

	s.leave(1)

	if !s.players[1].away() {
		t.Error("player is not marked away after leaving")
	}
	if got := s.edgeOwner[edge]; got != 1 {
		t.Errorf("edge owner = %d during the grace period, want 1", got)
	}
	if got := s.stationOwner[1]; got != 1 {
		t.Errorf("station owner = %d during the grace period, want 1", got)
	}
}

// 유예가 끝나면 그때야 지우고 땅을 놓는다.
func TestTheSeatIsReleasedOnceTheGraceExpires(t *testing.T) {
	t.Parallel()

	s, g := placed(t, map[PlayerID]Move{1: arriving(0, 1)}, 1)
	s.advance()
	edge := edgeIndex(t, g, 0, 1)

	s.leave(1)

	for range GraceTicks - 1 {
		s.advance()
	}
	if _, still := s.players[1]; !still {
		t.Fatal("player was evicted one tick early")
	}

	s.advance()

	if _, still := s.players[1]; still {
		t.Error("player is still here after the grace expired")
	}
	if got := s.edgeOwner[edge]; got != 0 {
		t.Errorf("edge owner = %d after eviction, want nobody", got)
	}
	if got := s.stationOwner[1]; got != 0 {
		t.Errorf("station owner = %d after eviction, want nobody", got)
	}
}

// 자리를 비운 사람은 멈춰 있다. 계속 달리면 접속이 끊긴 채로 땅을 먹는다.
func TestAwayPlayersDoNotMove(t *testing.T) {
	t.Parallel()

	s, _ := placed(t, map[PlayerID]Move{1: {From: 0, To: 1, TotalTicks: 10}}, 1)
	s.leave(1)

	before := s.players[1].move
	for range 3 {
		s.advance()
	}

	if s.players[1].move != before {
		t.Errorf("move = %+v while away, want %+v", s.players[1].move, before)
	}
}

func TestReturningWithTheSameNameRestoresEverything(t *testing.T) {
	t.Parallel()

	s, g := placed(t, map[PlayerID]Move{1: arriving(0, 1)}, 1)
	s.players[1].name = "hana"
	s.advance()

	edge := edgeIndex(t, g, 0, 1)
	position := s.players[1].move
	s.leave(1)
	s.advance()

	reply := make(chan Session, 1)
	s.join(command{kind: cmdJoin, name: "hana", replySession: reply})

	session := <-reply
	if session.ID != 1 {
		t.Fatalf("id after returning = %d, want 1", session.ID)
	}
	if s.players[1].away() {
		t.Error("player is still marked away after returning")
	}
	if s.players[1].move != position {
		t.Errorf("move = %+v after returning, want %+v", s.players[1].move, position)
	}
	if got := s.edgeOwner[edge]; got != 1 {
		t.Errorf("edge owner = %d after returning, want 1", got)
	}

	// 돌아온 사람은 다시 움직여야 한다.
	s.advance()
	if s.players[1].move == position {
		t.Error("player did not resume moving after returning")
	}
}

// 유예가 지난 뒤 같은 이름으로 오면 새 사람이다. 자리를 무한정 잡아둘 수는 없다.
func TestReturningTooLateStartsFresh(t *testing.T) {
	t.Parallel()

	s, _ := placed(t, map[PlayerID]Move{1: arriving(0, 1)}, 1)
	s.players[1].name = "hana"

	s.leave(1)
	for range GraceTicks {
		s.advance()
	}

	reply := make(chan Session, 1)
	s.join(command{kind: cmdJoin, name: "hana", replySession: reply})

	if session := <-reply; session.ID == 1 {
		t.Error("the old seat was handed back after the grace expired")
	}
}

// 접속 중인 사람의 이름을 대도 그 자리를 가져가지 못한다.
// 자리를 넘기는 것은 끊긴 사람에 한해서다.
func TestJoiningWithAConnectedPlayersNameCreatesANewPlayer(t *testing.T) {
	t.Parallel()

	s, _ := placed(t, map[PlayerID]Move{1: arriving(0, 1)}, 1)
	s.players[1].name = "hana"
	s.subs[1] = make(chan Snapshot, snapshotBuffer)

	reply := make(chan Session, 1)
	s.join(command{kind: cmdJoin, name: "hana", replySession: reply})

	if session := <-reply; session.ID == 1 {
		t.Error("took over the seat of a player who is still connected")
	}
	if len(s.order) != 2 {
		t.Errorf("players = %d, want 2", len(s.order))
	}
}

// 이름이 비어 있으면 아무 자리도 물려받지 않는다. 이름 없는 접속끼리
// 서로의 자리를 가져가면 누구의 것인지 알 수 없게 된다.
func TestAnEmptyNameNeverInheritsASeat(t *testing.T) {
	t.Parallel()

	s, _ := placed(t, map[PlayerID]Move{1: arriving(0, 1)}, 1)
	s.players[1].name = ""
	s.leave(1)

	reply := make(chan Session, 1)
	s.join(command{kind: cmdJoin, name: "", replySession: reply})

	if session := <-reply; session.ID == 1 {
		t.Error("an empty name inherited an away player's seat")
	}
}

// 자리를 비운 사람의 땅은 점수에 그대로 남는다. 끊겼다고 점수가 사라지면
// 유예를 두는 의미가 없다.
func TestAwayPlayersKeepTheirScore(t *testing.T) {
	t.Parallel()

	s, _ := placed(t, map[PlayerID]Move{1: arriving(0, 1)}, 1)
	s.advance()

	before := s.scores()[1]
	s.leave(1)

	if got := s.scores()[1]; got != before {
		t.Errorf("score while away = %d, want %d", got, before)
	}
}
