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

const (
	// DefaultNeedTicks는 구간 하나를 지나는 데 걸리는 틱 수다.
	// MVP에서는 모든 구간이 같다.
	DefaultNeedTicks = 3

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
	From      graph.StationID `json:"from"`
	To        graph.StationID `json:"to"`
	Progress  int             `json:"progress"`
	NeedTicks int             `json:"needTicks"`
}

type PlayerState struct {
	ID   PlayerID `json:"id"`
	Name string   `json:"name"`
	Move Move     `json:"move"`
}

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
	graph     *graph.Graph
	needTicks int
	commands  chan command
}

// New는 루프를 만들지만 돌리지는 않는다. 상태가 살아나는 것은 Run부터다 —
// 그 전에 Join을 부르면 명령이 채널 버퍼에 쌓여 있다가 ctx 취소로 실패한다.
//
// g는 이 뒤로 수정하지 말 것. 루프와 모든 연결이 같은 *Graph를 동시에 읽는데,
// 아무도 잠그지 않는다. 안 변한다는 것이 그 전제다.
//
// needTicks는 구간 하나를 지나는 데 걸리는 틱 수다. 1보다 작으면
// DefaultNeedTicks로 되돌린다.
func New(g *graph.Graph, needTicks int) *Loop {
	if needTicks < 1 {
		needTicks = DefaultNeedTicks
	}
	return &Loop{
		graph:     g,
		needTicks: needTicks,
		commands:  make(chan command, commandBuffer),
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
		return Session{}, ctx.Err()
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

// Leave는 플레이어를 지우고 그 사람의 구독 채널을 닫는다. 가진 땅도 함께 놓는다.
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
	st := newState(l.graph, l.needTicks)
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
}

type state struct {
	graph     *graph.Graph
	needTicks int

	tick   uint64
	nextID PlayerID

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

func newState(g *graph.Graph, needTicks int) *state {
	return &state{
		graph:        g,
		needTicks:    needTicks,
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
	s.players[id] = &player{
		name: c.name,
		move: Move{From: from, To: neighbors[0], NeedTicks: s.needTicks},
	}
	s.order = append(s.order, id)

	ch := make(chan Snapshot, snapshotBuffer)
	s.subs[id] = ch
	c.replySession <- Session{ID: id, Snapshots: ch}

	// 다음 틱까지 화면이 비어 있지 않게 현재 상태를 바로 한 번 보낸다.
	send(ch, s.snapshot())
}

func (s *state) leave(id PlayerID) {
	if _, ok := s.players[id]; !ok {
		return
	}

	delete(s.players, id)
	s.order = slices.DeleteFunc(s.order, func(other PlayerID) bool { return other == id })

	// 나간 사람의 땅은 놓는다. 다시 들어오면 새 ID를 받으므로, 그대로 두면
	// 주인 없는 색이 지도에 쌓이기만 한다.
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
		From:      m.To,
		To:        m.From,
		Progress:  m.NeedTicks - m.Progress,
		NeedTicks: m.NeedTicks,
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

	// 1단계: 전원을 옮기고 도착만 모아 둔다. 여기서 점령을 판정하지 않는 것이
	// 중요하다 — 옮기는 김에 그 자리에서 판정하면 s.order 앞쪽 플레이어가 먼저
	// 먹게 되고, 접속 순서가 승패를 가른다.
	var arrivals []arrival

	for _, id := range s.order {
		p := s.players[id]
		p.move.Progress++

		if p.move.Progress < p.move.NeedTicks {
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

		p.move = Move{From: p.move.To, To: next, NeedTicks: s.needTicks}
	}

	// 2단계: 전원이 다 움직인 뒤에 판정한다.
	s.claim(arrivals)
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
	players := make([]PlayerState, 0, len(s.order))
	for _, id := range s.order {
		p := s.players[id]
		players = append(players, PlayerState{ID: id, Name: p.name, Move: p.move})
	}

	return Snapshot{
		Tick:    s.tick,
		Players: players,
		// 복사한다. 방송된 스냅샷은 구독자 여럿이 같은 슬라이스를 나눠 가지므로,
		// 원본을 그대로 실으면 다음 틱의 변경이 이미 보낸 스냅샷까지 바꿔 놓는다.
		EdgeOwners:    slices.Clone(s.edgeOwner),
		StationOwners: slices.Clone(s.stationOwner),
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
