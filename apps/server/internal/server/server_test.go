package server

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jovid18/more-more/apps/server/internal/game"
	"github.com/jovid18/more-more/apps/server/internal/graph"
	"github.com/jovid18/more-more/apps/server/internal/hub"
)

func testMux(t *testing.T) *http.ServeMux {
	t.Helper()

	board := graph.Yamanote()
	loop := game.New(board)
	return newMux(t.Context(), hub.New(loop, board, TickInterval))
}

func TestHealthz(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	testMux(t).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got, want := rec.Body.String(), `{"status":"ok"}`; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

// 평범한 GET으로 /ws를 때리면 업그레이드가 아니므로 거절돼야 한다.
// 200이 나온다면 라우팅이 엉뚱한 핸들러에 걸린 것이다.
func TestWebSocketRouteRejectsPlainRequests(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	testMux(t).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ws", nil))

	if rec.Code == http.StatusOK {
		t.Errorf("status = %d, want an upgrade failure", rec.Code)
	}
}

// Run이 ctx 취소로 빠져나오는지 본다. 이 경로가 막히면 배포 때
// 컨테이너가 SIGTERM 뒤 강제 종료될 때까지 남는다.
func TestRunStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	// :0으로 열어 커널이 빈 포트를 고르게 한다. 병렬 테스트끼리 포트를 다투지 않는다.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, addr) }()

	// 서버가 뜬 뒤에 취소한다. 뜨기도 전에 취소하면 종료 경로를 지나지 않는다.
	waitForHealthz(t, addr)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil", err)
		}
	case <-time.After(shutdownTimeout + time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}

func waitForHealthz(t *testing.T, addr string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}

	t.Fatal("the server never became reachable")
}
