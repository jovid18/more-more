package graph

import (
	"errors"
	"testing"
)

func TestYamanoteIsAClosedRingOfThirty(t *testing.T) {
	t.Parallel()

	g := Yamanote()

	if got := g.Len(); got != 30 {
		t.Fatalf("stations = %d, want 30", got)
	}
	if got := len(g.Edges()); got != 30 {
		t.Errorf("edges = %d, want 30", got)
	}

	// 모든 역의 차수가 2여야 순환선이다. 하나라도 1이면 어딘가 끊겼고,
	// 3이면 갈림길이 생겨 Next가 ErrJunction을 낸다.
	for _, s := range g.Stations() {
		if got := len(g.Neighbors(s.ID)); got != 2 {
			t.Errorf("station %d (%s) has %d neighbours, want 2", s.ID, s.Name, got)
		}
	}
}

// 한 방향으로 계속 걸으면 30걸음 만에 출발점으로 돌아와야 한다.
// 중간에 되돌아오면 사이클이 아니라 왕복 경로라는 뜻이다.
func TestWalkingOneDirectionReturnsToStart(t *testing.T) {
	t.Parallel()

	g := Yamanote()
	prev, at := StationID(0), StationID(1)
	visited := map[StationID]bool{prev: true, at: true}

	for step := 2; step <= g.Len(); step++ {
		next, err := g.Next(at, prev)
		if err != nil {
			t.Fatalf("step %d: Next(%d, %d): %v", step, at, prev, err)
		}
		prev, at = at, next

		if at == 0 {
			if step != g.Len() {
				t.Fatalf("returned to start after %d steps, want %d", step, g.Len())
			}
			return
		}
		if visited[at] {
			t.Fatalf("step %d revisited station %d before closing the loop", step, at)
		}
		visited[at] = true
	}

	t.Fatalf("never returned to the start after %d steps", g.Len())
}

func TestNext(t *testing.T) {
	t.Parallel()

	line, err := New(
		[]Station{{ID: 0}, {ID: 1}, {ID: 2}},
		[]Edge{{A: 0, B: 1}, {A: 1, B: 2}},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Run("차수 2는 왔던 쪽이 아닌 곳으로 보낸다", func(t *testing.T) {
		t.Parallel()
		got, err := line.Next(1, 0)
		if err != nil || got != 2 {
			t.Fatalf("Next(1, 0) = %d, %v; want 2, nil", got, err)
		}
	})

	t.Run("가지 끝에서는 되돌아간다", func(t *testing.T) {
		t.Parallel()
		got, err := line.Next(2, 1)
		if err != nil || got != 1 {
			t.Fatalf("Next(2, 1) = %d, %v; want 1, nil", got, err)
		}
	})

	t.Run("갈림길은 ErrJunction", func(t *testing.T) {
		t.Parallel()
		junction, err := New(
			[]Station{{ID: 0}, {ID: 1}, {ID: 2}, {ID: 3}},
			[]Edge{{A: 0, B: 1}, {A: 1, B: 2}, {A: 1, B: 3}},
		)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if _, err := junction.Next(1, 0); !errors.Is(err, ErrJunction) {
			t.Fatalf("Next at a junction: %v, want ErrJunction", err)
		}
	})

	t.Run("이어지지 않은 prev는 거부한다", func(t *testing.T) {
		t.Parallel()
		if _, err := line.Next(1, 2222); err == nil {
			t.Fatal("Next with an unconnected prev succeeded, want an error")
		}
	})
}

func TestEdgeIndex(t *testing.T) {
	t.Parallel()

	g := Yamanote()

	t.Run("방향과 무관하게 같은 번호", func(t *testing.T) {
		t.Parallel()
		forward, ok := g.EdgeIndex(3, 4)
		if !ok {
			t.Fatal("EdgeIndex(3, 4) not found")
		}
		backward, ok := g.EdgeIndex(4, 3)
		if !ok {
			t.Fatal("EdgeIndex(4, 3) not found")
		}
		if forward != backward {
			t.Errorf("EdgeIndex(3,4) = %d but EdgeIndex(4,3) = %d", forward, backward)
		}
	})

	// 스냅샷의 소유 배열이 이 번호로 색인되므로, 번호가 Edges()의 자리와
	// 어긋나면 클라이언트가 엉뚱한 구간을 칠한다.
	t.Run("Edges()의 자리와 맞는다", func(t *testing.T) {
		t.Parallel()
		for want, edge := range g.Edges() {
			got, ok := g.EdgeIndex(edge.A, edge.B)
			if !ok || got != want {
				t.Fatalf("EdgeIndex(%d, %d) = %d, %v; want %d", edge.A, edge.B, got, ok, want)
			}
		}
	})

	t.Run("이어지지 않은 두 역은 없다", func(t *testing.T) {
		t.Parallel()
		if _, ok := g.EdgeIndex(0, 15); ok {
			t.Error("EdgeIndex found an edge between stations that are not adjacent")
		}
	})
}

func TestNewRejectsMalformedInput(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		stations []Station
		edges    []Edge
	}{
		"id가 인덱스와 다름": {[]Station{{ID: 0}, {ID: 7}}, nil},
		"없는 역 참조":     {[]Station{{ID: 0}}, []Edge{{A: 0, B: 5}}},
		"자기 자신 연결":    {[]Station{{ID: 0}}, []Edge{{A: 0, B: 0}}},
		"같은 구간 중복":    {[]Station{{ID: 0}, {ID: 1}}, []Edge{{A: 0, B: 1}, {A: 1, B: 0}}},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := New(tt.stations, tt.edges); err == nil {
				t.Error("New succeeded, want an error")
			}
		})
	}
}

// Stations는 복사본을 준다. 호출자가 건드려도 그래프가 안 변해야
// "만들고 나면 불변"이라는 약속이 성립한다.
func TestStationsReturnsACopy(t *testing.T) {
	t.Parallel()

	g := Yamanote()
	stations := g.Stations()
	stations[0].Name = "ruined"

	if got := g.Stations()[0].Name; got == "ruined" {
		t.Error("mutating the returned slice changed the graph")
	}
}
