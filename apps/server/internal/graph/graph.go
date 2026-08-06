// Package graph는 게임판이 되는 역·구간 그래프를 담는다.
//
// 순수 데이터다 — goroutine도 채널도 없고, 만들어진 뒤로는 바뀌지 않는다.
// 그래서 게임 루프와 여러 연결이 같은 *Graph를 동시에 읽어도 안전하다.
// 여기에 가변 상태(점령 현황 같은 것)를 넣지 말 것. 그건 game 패키지의 몫이고,
// 거기 넣는 순간 이 타입도 잠금이 필요해진다.
package graph

import (
	"errors"
	"fmt"
	"slices"
)

// StationID는 Graph.Stations()의 인덱스다. 0부터 빈틈없이 이어진다.
type StationID int

type Station struct {
	ID     StationID `json:"id"`
	Name   string    `json:"name"`
	Romaji string    `json:"romaji"`
}

// Edge는 방향이 없다. A < B로 정규화해 보관하므로 같은 구간이 두 번 들어가지 않는다.
type Edge struct {
	A StationID `json:"a"`
	B StationID `json:"b"`

	// TravelTicks는 이 구간을 건너는 데 걸리는 틱 수다. 이 모델에서 그것이 곧
	// 구간의 길이다 — 좌표가 없으므로 거리를 시간으로 표현한다. 1 이상이어야 한다.
	TravelTicks int `json:"travelTicks"`
}

// edgeKey는 구간을 찾는 열쇠다. Edge를 그대로 쓰지 않는 이유는 TravelTicks가
// 붙어 있어서다 — 값이 다르면 같은 구간을 못 찾는다.
type edgeKey struct {
	a, b StationID
}

func keyFor(a, b StationID) edgeKey {
	if a > b {
		a, b = b, a
	}
	return edgeKey{a: a, b: b}
}

// ErrJunction은 차수가 3 이상인 역에서 다음 행선지를 물었을 때 나온다.
// 갈림길에서는 플레이어가 골라야 하므로 그래프 혼자 정할 수 없다.
var ErrJunction = errors.New("graph: junction needs a choice")

type Graph struct {
	stations []Station
	edges    []Edge
	adj      [][]StationID // 인덱스 = StationID

	// 구간에서 edges 슬라이스의 인덱스로. 점령 판정이 매 틱 "방금 지나온 구간이
	// 몇 번인가"를 묻고, 도착할 때마다 "다음 구간이 몇 틱인가"를 물으므로 미리 만들어 둔다.
	index map[edgeKey]int
}

// New는 역과 구간을 검사한 뒤 그래프를 만든다.
//
// 역 ID는 슬라이스 인덱스와 같아야 한다. 이 규칙 덕분에 인접 리스트를 맵이 아니라
// 슬라이스로 둘 수 있고, 매 틱 도는 코드에서 해시를 타지 않는다.
func New(stations []Station, edges []Edge) (*Graph, error) {
	for i, s := range stations {
		if s.ID != StationID(i) {
			return nil, fmt.Errorf("graph: station %d has id %d, want %d", i, s.ID, i)
		}
	}

	g := &Graph{
		stations: slices.Clone(stations),
		edges:    make([]Edge, 0, len(edges)),
		adj:      make([][]StationID, len(stations)),
		index:    make(map[edgeKey]int, len(edges)),
	}

	for _, e := range edges {
		if !g.Contains(e.A) || !g.Contains(e.B) {
			return nil, fmt.Errorf("graph: edge %d-%d refers to an unknown station", e.A, e.B)
		}
		if e.A == e.B {
			return nil, fmt.Errorf("graph: station %d has an edge to itself", e.A)
		}
		// 0틱이면 플레이어가 그 구간에서 영원히 도착하지 못하거나 순간이동한다.
		// 어느 쪽이든 조용히 넘기면 원인을 찾기 어려운 버그가 된다.
		if e.TravelTicks < 1 {
			return nil, fmt.Errorf("graph: edge %d-%d has travelTicks %d, want at least 1", e.A, e.B, e.TravelTicks)
		}

		if e.A > e.B {
			e.A, e.B = e.B, e.A
		}
		key := keyFor(e.A, e.B)
		if _, taken := g.index[key]; taken {
			return nil, fmt.Errorf("graph: edge %d-%d appears twice", e.A, e.B)
		}

		g.index[key] = len(g.edges)
		g.edges = append(g.edges, e)
		g.adj[e.A] = append(g.adj[e.A], e.B)
		g.adj[e.B] = append(g.adj[e.B], e.A)
	}

	return g, nil
}

func (g *Graph) Contains(id StationID) bool {
	return id >= 0 && int(id) < len(g.stations)
}

// Stations와 Edges는 복사본을 준다. 호출자가 들고 있는 슬라이스를 건드려도
// 그래프가 망가지지 않아야, 이 타입을 "만들고 나면 안 변한다"고 말할 수 있다.
// 접속할 때 한 번씩만 부르는 경로라 복사 비용은 문제되지 않는다.
func (g *Graph) Stations() []Station { return slices.Clone(g.stations) }
func (g *Graph) Edges() []Edge       { return slices.Clone(g.edges) }

func (g *Graph) Len() int       { return len(g.stations) }
func (g *Graph) EdgeCount() int { return len(g.edges) }

// EdgeIndex는 두 역을 잇는 구간의 번호를 돌려준다. 방향은 상관없다.
// 스냅샷의 소유 배열이 이 번호로 색인되므로, 클라이언트가 받는 edges 순서와 같다.
func (g *Graph) EdgeIndex(a, b StationID) (int, bool) {
	i, ok := g.index[keyFor(a, b)]
	return i, ok
}

// TravelTicks는 두 역을 잇는 구간을 건너는 데 걸리는 틱 수다. 방향은 상관없다.
// 이어지지 않은 두 역이면 false.
func (g *Graph) TravelTicks(a, b StationID) (int, bool) {
	i, ok := g.index[keyFor(a, b)]
	if !ok {
		return 0, false
	}
	return g.edges[i].TravelTicks, true
}

// Neighbors는 id에 붙은 역들을 돌려준다. 반환된 슬라이스를 수정하지 말 것 —
// 매 틱 도는 경로라 여기서는 복사하지 않는다.
func (g *Graph) Neighbors(id StationID) []StationID {
	if !g.Contains(id) {
		return nil
	}
	return g.adj[id]
}

// Next는 prev에서 from으로 들어온 이동이 이어서 갈 역을 돌려준다.
//
// 차수가 2인 역에서는 "왔던 쪽이 아닌 곳"이 유일하게 정해진다. 야마노테선은
// 전 구간이 차수 2라 MVP에서는 이것으로 충분하다.
//
// 차수 1인 역(가지 끝)은 prev를 그대로 돌려준다 — 되돌아가는 것 말고 길이 없다.
// 차수 3 이상이면 ErrJunction이다. 가지 노선이 붙으면 여기가 플레이어의
// 선택 지점이 되고, 그 입력을 받는 것이 이 함수를 부르는 쪽의 일이 된다.
func (g *Graph) Next(from, prev StationID) (StationID, error) {
	if !g.Contains(from) {
		return 0, fmt.Errorf("graph: unknown station %d", from)
	}

	neighbors := g.adj[from]
	switch len(neighbors) {
	case 0:
		return 0, fmt.Errorf("graph: station %d has no neighbours", from)
	case 1:
		return neighbors[0], nil
	case 2:
		if neighbors[0] == prev {
			return neighbors[1], nil
		}
		if neighbors[1] == prev {
			return neighbors[0], nil
		}
		// prev가 이웃이 아니다. 호출자가 이어지지 않은 구간을 넘겼다는 뜻이다.
		return 0, fmt.Errorf("graph: %d is not adjacent to %d", prev, from)
	default:
		return 0, fmt.Errorf("%w: station %d has %d neighbours", ErrJunction, from, len(neighbors))
	}
}
