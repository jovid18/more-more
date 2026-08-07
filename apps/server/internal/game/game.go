// Package game은 게임 상태를 소유한다.
//
// 상태를 읽거나 쓰는 코드는 전부 Loop.Run 안에서 돈다. Run은 goroutine 하나이고,
// 바깥에서는 Join·Leave·Reverse 메서드만 부른다. 그 메서드들은 명령을 채널에
// 밀어 넣을 뿐 상태를 건드리지 않는다.
//
// 그래서 이 패키지에는 mutex가 없다. 없어도 되는 게 아니라, 필요해졌다면
// 어딘가에서 상태 소유권이 샌 것이다. 그때 잠그지 말고 새는 곳을 찾을 것.
package game

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/jovid18/more-more/apps/server/internal/graph"
)

// Phase는 판이 진행 중인지 끝났는지다.
type Phase string

const (
	PhasePlaying  Phase = "playing"
	PhaseFinished Phase = "finished"
)

const (
	// RoundTicks는 한 판의 길이다. 2Hz에서 600틱이면 5분이고,
	// 야마노테 한 바퀴가 45초이므로 여섯 바퀴 남짓 돌 수 있다.
	RoundTicks = 600

	// FinishedTicks는 결과를 보여주고 다음 판을 시작하기까지의 시간이다.
	FinishedTicks = 20

	// GraceTicks는 끊긴 플레이어의 자리를 붙들어 두는 시간이다. 2Hz에서 30초.
	//
	// 새로고침 한 번에 판을 통째로 잃지 않을 만큼은 길어야 하고, 나간 사람의 땅이
	// 지도를 오래 잠그지 않을 만큼은 짧아야 한다. 한 바퀴가 45초이므로
	// 그 안에 돌아오면 판을 이어갈 수 있다.
	GraceTicks = 60

	// abandonTimeout은 Join이 포기한 뒤 등록 결과를 기다리는 시간이다.
	abandonTimeout = 5 * time.Second

	// 명령 채널 버퍼. 루프는 매 순간 select로 명령을 받으므로 보통 비어 있다.
	// 버퍼는 틱 처리 중에 들어온 입력이 연결 goroutine을 붙잡지 않게 하는 완충일 뿐이다.
	commandBuffer = 32

	// 구독자 채널 버퍼. 1이면 충분하다 — 스냅샷은 전체 상태라
	// 밀린 것을 쌓아둘 이유가 없고, 최신 하나만 있으면 된다.
	snapshotBuffer = 1
)

type PlayerID int64

// Move는 플레이어가 어느 구간의 어디쯤인지를 정수로만 나타낸다.
//
// 좌표가 없는 것이 핵심이다. 서버는 "3틱 중 1틱째"까지만 알고, 그 사이를
// 부드럽게 잇는 것은 클라이언트가 보간으로 한다. 덕분에 틱이 느려도
// 화면은 매끄럽고, 판정은 정수 비교라 애매한 경계가 생기지 않는다.
type Move struct {
	From graph.StationID `json:"from"`
	To   graph.StationID `json:"to"`

	// Progress는 몇 틱째인지, TotalTicks는 몇 틱이 걸리는지다. 둘은 분자와 분모로
	// 함께 읽힌다 — 클라이언트는 Progress/TotalTicks 비율로 위치를 낸다.
	//
	// TotalTicks는 출발할 때 그 구간의 graph.Edge.TravelTicks를 베껴 온 것이다.
	// 베끼는 이유는 나중에 플레이어별 속도 보정이 생기면 이 값이 구간의 기본값과
	// 갈라지기 때문이고, 그때도 클라이언트는 스냅샷만 보고 그릴 수 있어야 한다.
	Progress   int `json:"progress"`
	TotalTicks int `json:"totalTicks"`
}

type PlayerState struct {
	ID    PlayerID `json:"id"`
	Name  string   `json:"name"`
	Move  Move     `json:"move"`
	Score int      `json:"score"`

	// Away는 연결이 끊겨 유예 시간 안에 있다는 뜻이다. 그 자리에 멈춰 있고,
	// 가진 땅은 그대로다. 돌아오지 않으면 유예가 끝날 때 사라진다.
	Away bool `json:"away"`
}

// Scoring은 점수 가중치다. 밸런스를 조절하는 지점을 한 곳에 모아 둔다.
//
// 역을 차수로 매기는 것은 README가 정한 기준이다 — 연결된 노선이 많은 허브일수록
// 값이 나가야 한다. 야마노테선은 전 역의 차수가 2라 지금은 균일하지만, 가지 노선이
// 붙어 차수가 갈리는 순간 손댈 것 없이 허브가 비싸진다.
//
// 구간을 길이로 매기는 것도 같은 이치다. 긴 구간은 건너는 데 오래 걸리므로
// 그만큼 값이 나가야 위험을 감수할 이유가 생긴다.
type Scoring struct {
	// PerStationDegree는 역의 차수 하나당 점수다.
	PerStationDegree int
	// PerEdgeTick은 구간의 TravelTicks 하나당 점수다.
	PerEdgeTick int
}

// DefaultScoring은 야마노테선을 기준으로 잡은 값이다.
//
// 역은 차수 2 × 5 = 10점, 구간은 3틱 × 2 = 6점. 역이 구간보다 비싼 것은 역이
// 거점이고 구간은 지나가며 칠하는 땅이기 때문이다. 전 구간·전 역을 다 가지면 480점.
//
// 근거 있는 숫자가 아니라 시작점이다. 실제로 여러 명이 돌려 보고 고칠 자리다.
var DefaultScoring = Scoring{PerStationDegree: 5, PerEdgeTick: 2}

// Snapshot은 한 틱의 완전한 상태다. 델타가 아니다 —
// 아무 틱에나 받아도 그것만으로 전체를 그릴 수 있어서 재접속이 공짜로 해결된다.
//
// 한 번 방송된 Snapshot은 모든 구독자가 같은 슬라이스를 공유한다. 받는 쪽에서
// 수정하지 말 것.
type Snapshot struct {
	Tick    uint64        `json:"tick"`
	Players []PlayerState `json:"players"`

	// 인덱스는 구간·역 번호이고, 값은 주인의 PlayerID다. 0은 주인이 없다는 뜻이다
	// (PlayerID는 1부터 나간다). 구간 번호는 welcome으로 보낸 edges 순서와 같다.
	//
	// 이름에 Owners를 붙인 것은 welcome의 edges(지도 구간 목록)와 구별하기
	// 위해서다. 같은 키에 다른 의미를 담으면 클라이언트가 조용히 잘못 읽는다.
	EdgeOwners    []PlayerID `json:"edgeOwners"`
	StationOwners []PlayerID `json:"stationOwners"`

	Phase Phase `json:"phase"`

	// TicksLeft는 지금 국면이 끝나기까지 남은 틱이다. playing이면 판이 끝날 때까지,
	// finished면 다음 판이 시작될 때까지. 클라이언트는 둘 다 같은 카운트다운으로 그린다.
	TicksLeft int `json:"ticksLeft"`

	// Winner는 끝난 판의 승자다. 0은 아직 안 끝났거나 무승부라는 뜻이다.
	Winner PlayerID `json:"winner"`
}

// Session은 접속한 플레이어에게 주어지는 손잡이다.
// Snapshots는 플레이어가 나가거나 루프가 끝나면 닫힌다.
type Session struct {
	ID        PlayerID
	Snapshots <-chan Snapshot
}

type commandKind int

const (
	cmdJoin commandKind = iota
	cmdLeave
	cmdReverse
	cmdSnapshot
)

type command struct {
	kind commandKind
	id   PlayerID // leave, reverse
	name string   // join

	// 응답 채널은 전부 버퍼 1이다. 요청한 쪽이 ctx 취소로 먼저 사라져도
	// 루프가 응답을 보내다 갇히지 않는다.
	replySession  chan Session  // join
	replySnapshot chan Snapshot // snapshot
}

type Loop struct {
	graph    *graph.Graph
	scoring  Scoring
	commands chan command
}

// New는 루프를 만들지만 돌리지는 않는다. 상태가 살아나는 것은 Run부터다 —
// 그 전에 Join을 부르면 명령이 채널 버퍼에 쌓여 있다가 ctx 취소로 실패한다.
//
// g는 이 뒤로 수정하지 말 것. 루프와 모든 연결이 같은 *Graph를 동시에 읽는데,
// 아무도 잠그지 않는다. 안 변한다는 것이 그 전제다.
//
// 구간마다 몇 틱이 걸리는지는 g가 들고 있다 — 이 함수에 설정으로 넘기지 않는다.
// 길이는 지도의 성질이고, 게임의 설정이 아니다.
func New(g *graph.Graph) *Loop {
	return &Loop{
		graph:    g,
		scoring:  DefaultScoring,
		commands: make(chan command, commandBuffer),
	}
}

// Join은 플레이어를 등록하고 스냅샷 구독을 돌려준다.
// 나갈 때는 반드시 Leave를 부를 것 — 부르지 않으면 루프가 끝날 때까지 남는다.
func (l *Loop) Join(ctx context.Context, name string) (Session, error) {
	reply := make(chan Session, 1)

	if err := l.send(ctx, command{kind: cmdJoin, name: name, replySession: reply}); err != nil {
		return Session{}, err
	}

	select {
	case s := <-reply:
		return s, nil

	case <-ctx.Done():
		// 명령은 이미 큐에 들어갔다. 루프가 등록해 버리면 아무도 나간다고
		// 알려주지 않아 유령 플레이어가 남는다. 뒤처리를 떼어 보내고 나간다 —
		// 여기서 기다리면 종료 경로가 그만큼 늘어진다.
		go l.abandonJoin(reply)
		return Session{}, ctx.Err()
	}
}

// abandonJoin은 Join이 포기한 뒤에도 등록됐을지 모르는 플레이어를 지운다.
//
// 응답 채널은 버퍼 1이라 루프는 이미 보냈거나 곧 보낸다. 그 안에 안 오면
// 루프가 명령을 처리하지 못한 것이고(대개 종료 중), 등록되지 않았으므로 할 일이 없다.
func (l *Loop) abandonJoin(reply <-chan Session) {
	select {
	case s := <-reply:
		ctx, cancel := context.WithTimeout(context.Background(), abandonTimeout)
		defer cancel()
		if err := l.Leave(ctx, s.ID); err != nil {
			slog.Warn("abandonJoin: leave failed", "player", s.ID, "err", err)
		}
	case <-time.After(abandonTimeout):
	}
}

// Snapshot은 지금 상태를 한 장 떠서 돌려준다.
//
// 읽기도 다른 명령과 똑같이 채널을 타고 루프에 들어간다. 상태를 직접 읽는
// 지름길을 내면 그 순간 잠금이 필요해지기 때문이다. 명령 채널은 FIFO라
// Reverse 다음에 부르면 반드시 그 결과가 보인다.
func (l *Loop) Snapshot(ctx context.Context) (Snapshot, error) {
	reply := make(chan Snapshot, 1)

	if err := l.send(ctx, command{kind: cmdSnapshot, replySnapshot: reply}); err != nil {
		return Snapshot{}, err
	}

	select {
	case s := <-reply:
		return s, nil
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	}
}

// Leave는 연결이 끊겼다고 알린다. 플레이어를 즉시 지우지는 않는다 —
// 그 자리에 멈춰 세우고 GraceTicks 동안 자리와 땅을 붙들어 둔다. 같은 이름으로
// 다시 Join하면 그대로 돌려받고, 안 돌아오면 유예가 끝날 때 사라진다.
//
// 이미 없는 플레이어를 넘겨도 조용히 넘어간다 — 연결이 끊기는 경로가 여럿이라
// 두 번 불릴 수 있고, 그것 때문에 실패할 이유는 없다.
//
// ctx는 명령을 넣을 때까지만 쓴다. 끊긴 연결을 정리하는 자리에서는 이미 취소된
// ctx를 물려받기 쉬우므로, 부르는 쪽에서 context.WithoutCancel로 취소를 떼어낼 것.
func (l *Loop) Leave(ctx context.Context, id PlayerID) error {
	return l.send(ctx, command{kind: cmdLeave, id: id})
}

// Reverse는 진행 방향을 뒤집는다. 야마노테선에는 갈림길이 없으므로
// M1에서 플레이어가 할 수 있는 선택은 이것 하나다.
func (l *Loop) Reverse(ctx context.Context, id PlayerID) error {
	return l.send(ctx, command{kind: cmdReverse, id: id})
}

func (l *Loop) send(ctx context.Context, c command) error {
	select {
	case l.commands <- c:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Run은 ctx가 취소될 때까지 상태를 소유한다. ticks에서 신호가 올 때마다
// 한 틱 진행하고 전원에게 스냅샷을 방송한다.
//
// ticks를 주입받는 이유는 테스트다. 실제 서버는 time.Ticker의 채널을 넘기고,
// 테스트는 직접 만든 채널에 신호를 넣어 시간을 기다리지 않고 틱을 진행시킨다.
func (l *Loop) Run(ctx context.Context, ticks <-chan time.Time) {
	st := newState(l.graph, l.scoring)
	// 루프가 끝나면 구독 채널을 전부 닫는다. 각 연결의 쓰기 goroutine이
	// 닫힌 채널을 보고 스스로 빠져나가는 것이 종료 신호다.
	defer st.closeAll()

	for {
		select {
		case <-ctx.Done():
			return

		case c := <-l.commands:
			st.apply(c)

		case <-ticks:
			st.advance()
			st.broadcast()
		}
	}
}

// ─── 여기서부터는 Run goroutine만 만진다 ─────────────────────────

type player struct {
	name string
	move Move

	// awayUntil은 연결이 끊긴 플레이어를 지울 틱이다. 0이면 접속 중이다.
	// 절대 틱으로 두면 판정이 s.tick과의 비교 한 줄로 끝난다.
	awayUntil uint64
}

func (p *player) away() bool { return p.awayUntil != 0 }

type state struct {
	graph *graph.Graph

	tick   uint64
	nextID PlayerID

	phase Phase
	// roundTick은 지금 국면에서 흐른 틱이다. 판이 바뀔 때마다 0으로 돌아간다.
	// tick은 접속 시점부터 단조증가하므로 둘을 섞지 않는다.
	roundTick int
	winner    PlayerID

	// 점수 가중치를 미리 곱해 둔다. 인덱스 = 구간·역 번호.
	// 매 틱 스냅샷마다 세는 값이라 graph.Edges()를 반복해서 복사하지 않으려는 것이다.
	edgeValue    []int
	stationValue []int

	// order는 맵 순회 순서가 무작위라서 둔다. 스냅샷의 플레이어 순서가
	// 매 틱 바뀌면 클라이언트가 diff를 볼 수 없고 테스트도 흔들린다.
	order   []PlayerID
	players map[PlayerID]*player
	subs    map[PlayerID]chan Snapshot

	// 인덱스로 찾는다. 맵이 아니라 슬라이스인 것은 매 틱 통째로 복사해
	// 스냅샷에 싣기 때문이다.
	edgeOwner    []PlayerID
	stationOwner []PlayerID
}

func newState(g *graph.Graph, scoring Scoring) *state {
	// 가중치는 지도가 안 변하므로 한 번만 곱해 둔다.
	edges := g.Edges()
	edgeValue := make([]int, len(edges))
	for i, e := range edges {
		edgeValue[i] = e.TravelTicks * scoring.PerEdgeTick
	}

	stationValue := make([]int, g.Len())
	for i := range stationValue {
		degree := len(g.Neighbors(graph.StationID(i)))
		stationValue[i] = degree * scoring.PerStationDegree
	}

	return &state{
		graph:        g,
		phase:        PhasePlaying,
		edgeValue:    edgeValue,
		stationValue: stationValue,
		players:      make(map[PlayerID]*player),
		subs:         make(map[PlayerID]chan Snapshot),
		edgeOwner:    make([]PlayerID, g.EdgeCount()),
		stationOwner: make([]PlayerID, g.Len()),
	}
}

func (s *state) apply(c command) {
	switch c.kind {
	case cmdJoin:
		s.join(c)
	case cmdLeave:
		s.leave(c.id)
	case cmdReverse:
		s.reverse(c.id)
	case cmdSnapshot:
		c.replySnapshot <- s.snapshot()
	}
}

func (s *state) join(c command) {
	// 유예 안에 같은 이름으로 돌아왔으면 있던 자리를 그대로 돌려준다.
	if id, ok := s.awayNamed(c.name); ok {
		s.players[id].awayUntil = 0

		ch := make(chan Snapshot, snapshotBuffer)
		s.subs[id] = ch
		c.replySession <- Session{ID: id, Snapshots: ch}
		send(ch, s.snapshot())

		slog.Info("player returned", "player", id, "name", c.name)
		return
	}

	s.nextID++
	id := s.nextID

	// 출발 위치를 ID로 흩뿌린다. 전원이 같은 역에서 시작하면 초반이 뭉쳐서
	// 아무 일도 일어나지 않는다.
	from := graph.StationID(int(id) % s.graph.Len())
	neighbors := s.graph.Neighbors(from)
	if len(neighbors) == 0 {
		slog.Error("join: station has no neighbours", "station", from)
		close(c.replySession)
		return
	}

	// 어느 이웃으로 출발하든 상관없다. 방향은 Reverse로 바꾼다.
	to := neighbors[0]
	total, ok := s.graph.TravelTicks(from, to)
	if !ok {
		// Neighbors가 이웃이라고 답했으니 구간이 있어야 한다. 없으면 그래프가 깨졌다.
		slog.Error("join: no edge between neighbouring stations", "from", from, "to", to)
		close(c.replySession)
		return
	}

	s.players[id] = &player{
		name: c.name,
		move: Move{From: from, To: to, TotalTicks: total},
	}
	s.order = append(s.order, id)

	ch := make(chan Snapshot, snapshotBuffer)
	s.subs[id] = ch
	c.replySession <- Session{ID: id, Snapshots: ch}

	// 다음 틱까지 화면이 비어 있지 않게 현재 상태를 바로 한 번 보낸다.
	send(ch, s.snapshot())
}

// awayNamed는 유예 안에 있는 같은 이름의 플레이어를 찾는다.
//
// 이름은 인증이 아니다. 남의 이름을 대면 그 자리를 가져갈 수 있다.
// 혼자 또는 아는 사람끼리 하는 동안은 이걸로 충분하고, 계정이 생기면
// 그때 토큰으로 바꾼다.
func (s *state) awayNamed(name string) (PlayerID, bool) {
	if name == "" {
		return 0, false
	}

	for _, id := range s.order {
		if p := s.players[id]; p.away() && p.name == name {
			return id, true
		}
	}
	return 0, false
}

// leave는 연결이 끊긴 플레이어를 멈춰 세우고 유예 시간을 건다.
// 자리도 땅도 그대로 두고, 구독 채널만 닫는다 — 소켓이 이미 없기 때문이다.
func (s *state) leave(id PlayerID) {
	p, ok := s.players[id]
	if !ok || p.away() {
		return
	}

	p.awayUntil = s.tick + GraceTicks

	if ch, ok := s.subs[id]; ok {
		close(ch)
		delete(s.subs, id)
	}
}

// remove는 유예가 끝난 플레이어를 지운다. 여기서야 땅을 놓는다.
func (s *state) remove(id PlayerID) {
	if _, ok := s.players[id]; !ok {
		return
	}

	delete(s.players, id)
	s.order = slices.DeleteFunc(s.order, func(other PlayerID) bool { return other == id })

	// 돌아오지 않았으므로 땅을 놓는다. 그대로 두면 주인 없는 색이 지도에 쌓이고,
	// 다시 들어와도 새 ID를 받으므로 되찾을 방법이 없다.
	for i, owner := range s.edgeOwner {
		if owner == id {
			s.edgeOwner[i] = 0
		}
	}
	for i, owner := range s.stationOwner {
		if owner == id {
			s.stationOwner[i] = 0
		}
	}

	if ch, ok := s.subs[id]; ok {
		close(ch)
		delete(s.subs, id)
	}
}

func (s *state) reverse(id PlayerID) {
	p, ok := s.players[id]
	if !ok {
		return
	}

	// 구간 한가운데서 방향을 뒤집는다. 지나온 거리가 남은 거리가 되므로
	// progress도 같이 뒤집어야 플레이어가 제자리에 머문다.
	// 3틱 구간의 1틱 지점에서 뒤집으면 반대편에서 2틱 지점이다.
	m := p.move
	p.move = Move{
		From:       m.To,
		To:         m.From,
		Progress:   m.TotalTicks - m.Progress,
		TotalTicks: m.TotalTicks,
	}
}

// evictExpired는 유예가 끝나도 돌아오지 않은 플레이어를 지운다.
func (s *state) evictExpired() {
	// remove가 s.order를 줄이므로 복사본을 훑는다.
	for _, id := range slices.Clone(s.order) {
		if p := s.players[id]; p != nil && p.away() && s.tick >= p.awayUntil {
			slog.Info("player evicted", "player", id, "name", p.name)
			s.remove(id)
		}
	}
}

// arrival은 이번 틱에 구간 하나를 끝낸 플레이어다.
type arrival struct {
	player  PlayerID
	edge    int
	station graph.StationID
}

func (s *state) advance() {
	s.tick++
	s.roundTick++

	// 유예가 끝난 사람을 먼저 치운다. 이번 틱의 판정에 유령이 끼지 않게 한다.
	s.evictExpired()

	// 결과를 보여주는 동안에는 아무도 움직이지 않는다. 다음 판이 시작될 때까지
	// 지도가 그대로 남아 있어야 누가 어떻게 이겼는지 볼 수 있다.
	if s.phase == PhaseFinished {
		if s.roundTick >= FinishedTicks {
			s.startRound()
		}
		return
	}

	// 1단계: 전원을 옮기고 도착만 모아 둔다. 여기서 점령을 판정하지 않는 것이
	// 중요하다 — 옮기는 김에 그 자리에서 판정하면 s.order 앞쪽 플레이어가 먼저
	// 먹게 되고, 접속 순서가 승패를 가른다.
	var arrivals []arrival

	for _, id := range s.order {
		p := s.players[id]

		// 자리를 비운 사람은 멈춰 있다. 계속 달리게 두면 접속이 끊긴 채로
		// 땅을 계속 먹는다.
		if p.away() {
			continue
		}

		p.move.Progress++

		if p.move.Progress < p.move.TotalTicks {
			continue
		}

		if edge, ok := s.graph.EdgeIndex(p.move.From, p.move.To); ok {
			arrivals = append(arrivals, arrival{player: id, edge: edge, station: p.move.To})
		}

		// 도착했다. 역에 머무는 상태는 두지 않고 곧바로 다음 구간에 올린다 —
		// 플레이어는 언제나 어느 구간 위에 있다.
		next, err := s.graph.Next(p.move.To, p.move.From)
		if err != nil {
			// 야마노테선에는 갈림길이 없으므로 여기 오면 그래프가 바뀐 것이다.
			// 멈춰 세우는 대신 왔던 길로 되돌린다.
			slog.Warn("advance: cannot pick the next station", "player", id, "at", p.move.To, "err", err)
			next = p.move.From
		}

		// 다음 구간의 길이는 지도에서 읽는다. 구간마다 다를 수 있으므로
		// 이전 구간의 값을 물려주면 안 된다.
		total, ok := s.graph.TravelTicks(p.move.To, next)
		if !ok {
			// 여기 오면 그래프가 깨진 것이다. 멈춰 세우기보다 직전 길이로 이어 붙인다.
			slog.Error("advance: no edge to the next station", "player", id, "from", p.move.To, "to", next)
			total = p.move.TotalTicks
		}

		p.move = Move{From: p.move.To, To: next, TotalTicks: total}
	}

	// 2단계: 전원이 다 움직인 뒤에 판정한다.
	s.claim(arrivals)

	// 3단계: 판이 끝났는지 본다. 마지막 틱의 점령까지 세고 나서 판정한다 —
	// 종료 직전에 뺏은 땅도 점수에 들어가야 한다.
	if s.roundTick >= RoundTicks {
		s.finishRound()
	}
}

// finishRound는 판을 닫고 승자를 정한다. 지도는 그대로 두어 결과를 보여준다.
func (s *state) finishRound() {
	s.phase = PhaseFinished
	s.roundTick = 0
	s.winner = 0

	// 동점이면 승자가 없다. 이기지 못한 것과 진 것을 구분하지 않는 편이,
	// 순위를 어떻게 가를지 지금 정하지 않아도 되게 한다.
	best := 0
	tied := false
	for id, score := range s.scores() {
		switch {
		case score > best:
			best, s.winner, tied = score, id, false
		case score == best && id != s.winner:
			tied = true
		}
	}
	if tied || best == 0 {
		s.winner = 0
	}

	slog.Info("round finished", "winner", s.winner, "score", best, "players", len(s.order))
}

// startRound는 지도를 비우고 새 판을 시작한다.
//
// 플레이어는 있던 자리에 그대로 둔다. 위치까지 되돌리면 화면에서 전원이
// 순간이동하고, 클라이언트 보간이 그 한 틱을 직선으로 이어 그린다.
func (s *state) startRound() {
	s.phase = PhasePlaying
	s.roundTick = 0
	s.winner = 0

	clear(s.edgeOwner)
	clear(s.stationOwner)

	slog.Info("round started", "players", len(s.order))
}

// scores는 플레이어별 점수를 센다. 주인 없는 땅은 0이므로 건너뛴다.
func (s *state) scores() map[PlayerID]int {
	scores := make(map[PlayerID]int, len(s.order))

	for index, owner := range s.edgeOwner {
		if owner != 0 {
			scores[owner] += s.edgeValue[index]
		}
	}
	for index, owner := range s.stationOwner {
		if owner != 0 {
			scores[owner] += s.stationValue[index]
		}
	}

	return scores
}

// ticksLeft는 지금 국면이 끝나기까지 남은 틱이다.
func (s *state) ticksLeft() int {
	limit := RoundTicks
	if s.phase == PhaseFinished {
		limit = FinishedTicks
	}
	return max(limit-s.roundTick, 0)
}

// claim은 이번 틱의 도착을 소유권으로 옮긴다.
//
// **규칙은 하나다. 한 틱에 정확히 한 명이 닿은 곳만 주인이 바뀐다.**
//
// 짧지만 두 가지가 여기서 따라 나온다. 첫째, 수비가 유리하다 — 지키러 온 주인과
// 뺏으러 온 사람이 같은 틱에 닿으면 아무 일도 없고, 주인이 그대로 남는다.
// 둘째, 뭉쳐 다니면 아무도 못 먹는다. 같은 구간을 나란히 달리는 두 사람은
// 서로를 계속 상쇄한다.
//
// 판정 순서에 기대지 않는 것도 요점이다. 각 구간·역은 서로 독립적으로 정해지므로
// 맵을 무슨 순서로 돌든 결과가 같다.
func (s *state) claim(arrivals []arrival) {
	if len(arrivals) == 0 {
		return
	}

	type claim struct {
		by    PlayerID
		count int
	}

	edges := make(map[int]claim, len(arrivals))
	stations := make(map[graph.StationID]claim, len(arrivals))

	for _, a := range arrivals {
		e := edges[a.edge]
		edges[a.edge] = claim{by: a.player, count: e.count + 1}

		st := stations[a.station]
		stations[a.station] = claim{by: a.player, count: st.count + 1}
	}

	for index, c := range edges {
		if c.count == 1 {
			s.edgeOwner[index] = c.by
		}
	}
	for id, c := range stations {
		if c.count == 1 {
			s.stationOwner[id] = c.by
		}
	}
}

func (s *state) snapshot() Snapshot {
	scores := s.scores()

	players := make([]PlayerState, 0, len(s.order))
	for _, id := range s.order {
		p := s.players[id]
		players = append(players, PlayerState{
			ID:    id,
			Name:  p.name,
			Move:  p.move,
			Score: scores[id],
			Away:  p.away(),
		})
	}

	return Snapshot{
		Tick:    s.tick,
		Players: players,
		// 복사한다. 방송된 스냅샷은 구독자 여럿이 같은 슬라이스를 나눠 가지므로,
		// 원본을 그대로 실으면 다음 틱의 변경이 이미 보낸 스냅샷까지 바꿔 놓는다.
		EdgeOwners:    slices.Clone(s.edgeOwner),
		StationOwners: slices.Clone(s.stationOwner),
		Phase:         s.phase,
		TicksLeft:     s.ticksLeft(),
		Winner:        s.winner,
	}
}

func (s *state) broadcast() {
	snap := s.snapshot()
	for _, id := range s.order {
		send(s.subs[id], snap)
	}
}

func (s *state) closeAll() {
	for id, ch := range s.subs {
		close(ch)
		delete(s.subs, id)
	}
}

// send는 절대 블로킹하지 않는다.
//
// 여기서 막히면 느린 연결 하나가 게임 전체의 틱을 멈춰 세운다. 버퍼가 차 있으면
// 밀린 스냅샷을 버리고 최신 것을 넣는다 — 스냅샷은 전체 상태라 중간을 건너뛰어도
// 잃는 정보가 없고, 뒤처진 클라이언트에게는 오래된 것보다 최신이 낫다.
func send(ch chan Snapshot, snap Snapshot) {
	if ch == nil {
		return
	}

	select {
	case ch <- snap:
		return
	default:
	}

	// 버퍼를 비우고 다시 시도한다. 두 단계 모두 비블로킹이라, 그 사이에
	// 소비자가 먼저 읽어가도 최악의 경우 이번 스냅샷 하나를 거르는 것으로 끝난다.
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- snap:
	default:
	}
}
