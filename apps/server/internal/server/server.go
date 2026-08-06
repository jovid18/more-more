// Package server는 HTTP 표면과 프로세스 수명을 담당한다.
//
// 게임 상태는 이 패키지에 없다. 상태는 game 패키지의 goroutine 하나가 통째로
// 소유하고, 여기서는 그것을 띄우고 내리는 일만 한다 — 핸들러가 상태를 직접
// 읽거나 쓰기 시작하면 그 순간 mutex가 필요해지고, 설계 전제가 무너진다.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jovid18/more-more/apps/server/internal/game"
	"github.com/jovid18/more-more/apps/server/internal/graph"
	"github.com/jovid18/more-more/apps/server/internal/hub"
)

const (
	// shutdownTimeout은 종료 신호를 받고 진행 중인 요청을 기다리는 시간이다.
	shutdownTimeout = 5 * time.Second

	// Slowloris 방어. 핸들러 자체에는 타임아웃을 걸지 않는다 —
	// WebSocket 연결은 오래 열려 있는 것이 정상이기 때문이다.
	readHeaderTimeout = 5 * time.Second

	// TickInterval은 시뮬레이션 주기다. 2Hz.
	//
	// 반응속도를 겨루는 게임이 아니라 전략 게임이라 이 정도로 충분하고,
	// 서버가 느린 만큼은 클라이언트 보간이 메운다. 구간당 3틱이므로
	// 역 사이가 1.5초, 야마노테 한 바퀴가 45초다.
	TickInterval = 500 * time.Millisecond
)

func newMux(ctx context.Context, h *hub.Hub) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	mux.HandleFunc("GET /ws", h.Handler(ctx))

	return mux
}

// Run은 ctx가 취소될 때까지 게임 루프와 HTTP 서버를 함께 돌린다.
//
// 종료 순서가 중요하다. 먼저 HTTP를 내려 열린 연결을 정리하고, 그다음 게임
// 루프가 구독 채널을 닫고 빠져나갈 때까지 기다린다. 반대로 하면 아직 살아 있는
// 연결이 이미 닫힌 채널을 보게 된다.
func Run(ctx context.Context, addr string) error {
	board := graph.Yamanote()
	loop := game.New(board, game.DefaultNeedTicks)

	ticker := time.NewTicker(TickInterval)
	defer ticker.Stop()

	// 게임 루프를 먼저 띄운다. HTTP가 열리기 전에 상태 소유자가 살아 있어야
	// 첫 접속이 아무도 없는 채널에 말을 걸지 않는다.
	loopCtx, stopLoop := context.WithCancel(ctx)
	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		loop.Run(loopCtx, ticker.C)
	}()

	// 어느 경로로 빠져나가든 루프를 세우고 끝날 때까지 기다린다.
	// defer는 LIFO라 아래 select의 Shutdown이 먼저 끝난 뒤에 돈다.
	defer func() {
		stopLoop()
		<-loopDone
	}()

	srv := &http.Server{
		Addr:              addr,
		Handler:           newMux(ctx, hub.New(loop, board, TickInterval)),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	// 버퍼 1. Shutdown 경로로 빠져도 이 goroutine이 채널에 갇히지 않는다.
	errc := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", addr, "tick", TickInterval, "stations", board.Len())
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		// ListenAndServe는 정상 종료일 때도 ErrServerClosed를 돌려준다.
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err

	case <-ctx.Done():
		slog.Info("shutting down")
		// ctx는 이미 취소됐으므로 Shutdown에 그대로 넘길 수 없다. 새로 만든다.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
