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
		[]Edge{{A: 0, B: 1, TravelTicks: 1}, {A: 1, B: 2, TravelTicks: 1}},
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
			[]Edge{{A: 0, B: 1, TravelTicks: 1}, {A: 1, B: 2, TravelTicks: 1}, {A: 1, B: 3, TravelTicks: 1}},
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

func TestTravelTicks(t *testing.T) {
	t.Parallel()

	g := Yamanote()

	t.Run("방향과 무관하게 같은 값", func(t *testing.T) {
		t.Parallel()
		forward, ok := g.TravelTicks(3, 4)
		if !ok {
			t.Fatal("TravelTicks(3, 4) not found")
		}
		backward, _ := g.TravelTicks(4, 3)
		if forward != backward {
			t.Errorf("TravelTicks(3,4) = %d but TravelTicks(4,3) = %d", forward, backward)
		}
	})

	// 실측치가 들어오면 이 단정이 깨진다. 그때 이 테스트를 지우는 것이
	// "구간 길이가 갈리기 시작했다"는 표시가 된다.
	t.Run("야마노테는 아직 전 구간이 같다", func(t *testing.T) {
		t.Parallel()
		for _, edge := range g.Edges() {
			if edge.TravelTicks != YamanoteTravelTicks {
				t.Fatalf("edge %d-%d has travelTicks %d, want %d",
					edge.A, edge.B, edge.TravelTicks, YamanoteTravelTicks)
			}
		}
	})

	t.Run("이어지지 않은 두 역은 없다", func(t *testing.T) {
		t.Parallel()
		if _, ok := g.TravelTicks(0, 15); ok {
			t.Error("TravelTicks found an edge between stations that are not adjacent")
		}
	})
}

// 구간마다 길이가 다른 그래프에서도 조회가 맞아야 한다. 야마노테선은 전부 같은
// 값이라 이 검사를 통과하지 못한다 — 값을 뒤섞어도 눈치채지 못하기 때문이다.
func TestTravelTicksWithVaryingLengths(t *testing.T) {
	t.Parallel()

	g, err := New(
		[]Station{{ID: 0}, {ID: 1}, {ID: 2}},
		[]Edge{{A: 0, B: 1, TravelTicks: 2}, {A: 1, B: 2, TravelTicks: 7}},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for _, tt := range []struct {
		a, b StationID
		want int
	}{
		{0, 1, 2},
		{1, 0, 2},
		{1, 2, 7},
		{2, 1, 7},
	} {
		got, ok := g.TravelTicks(tt.a, tt.b)
		if !ok || got != tt.want {
			t.Errorf("TravelTicks(%d, %d) = %d, %v; want %d", tt.a, tt.b, got, ok, tt.want)
		}
	}
}

func TestNewRejectsMalformedInput(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		stations []Station
		edges    []Edge
	}{
		"id가 인덱스와 다름": {[]Station{{ID: 0}, {ID: 7}}, nil},
		"없는 역 참조":     {[]Station{{ID: 0}}, []Edge{{A: 0, B: 5, TravelTicks: 1}}},
		"자기 자신 연결":    {[]Station{{ID: 0}}, []Edge{{A: 0, B: 0, TravelTicks: 1}}},
		"같은 구간 중복":    {[]Station{{ID: 0}, {ID: 1}}, []Edge{{A: 0, B: 1, TravelTicks: 1}, {A: 1, B: 0, TravelTicks: 1}}},
		// 0틱이면 도착 판정이 첫 틱에 걸려 순간이동하고, 음수면 영영 도착하지 못한다.
		"travelTicks 0": {[]Station{{ID: 0}, {ID: 1}}, []Edge{{A: 0, B: 1}}},
		"travelTicks 음수": {
			[]Station{{ID: 0}, {ID: 1}},
			[]Edge{{A: 0, B: 1, TravelTicks: -2}},
		},
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
