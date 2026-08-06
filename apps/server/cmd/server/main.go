// Command server runs the game server.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jovid18/more-more/apps/server/internal/server"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	// SIGINT/SIGTERM이 오면 ctx가 취소된다. 이 취소 하나가 HTTP 서버와
	// (앞으로 붙을) 게임 루프의 공통 종료 신호다. 종료 경로를 둘로 나누지 않는다.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := server.Run(ctx, *addr); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}
