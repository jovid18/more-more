// Package server는 HTTP 표면만 담당한다.
//
// 게임 상태는 이 패키지에 없다. 상태는 goroutine 하나가 통째로 소유하고,
// 여기서는 채널로만 이야기한다 — 핸들러가 상태를 직접 읽거나 쓰기 시작하면
// 그 순간 mutex가 필요해지고, 이 프로젝트의 설계 전제가 무너진다.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

const (
	// shutdownTimeout은 종료 신호를 받고 진행 중인 요청을 기다리는 시간이다.
	shutdownTimeout = 5 * time.Second

	// Slowloris 방어. 핸들러 자체에는 타임아웃을 걸지 않는다 —
	// WebSocket 연결은 오래 열려 있는 것이 정상이기 때문이다.
	readHeaderTimeout = 5 * time.Second
)

func newMux() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	return mux
}

// Run은 ctx가 취소될 때까지 HTTP 서버를 돌린다. 취소되면 진행 중인 요청을
// shutdownTimeout까지 기다린 뒤 반환한다.
func Run(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           newMux(),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	// 버퍼 1. Shutdown 경로로 빠져도 이 goroutine이 채널에 갇히지 않는다.
	errc := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", addr)
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
