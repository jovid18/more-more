// Package hub은 WebSocket 연결을 게임 루프에 잇는다.
//
// 연결 하나당 goroutine 두 개다 — 읽기와 쓰기. 둘 다 게임 상태를 직접 만지지
// 않고, 읽기는 Loop의 메서드를 부르고 쓰기는 구독 채널에서 받아 내보내기만 한다.
// 상태는 여전히 game 패키지의 goroutine 하나가 소유한다.
package hub

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/jovid18/more-more/apps/server/internal/game"
	"github.com/jovid18/more-more/apps/server/internal/graph"
)

const (
	// 메시지 하나를 내보내는 데 허용하는 시간. 여기서 막히는 연결은
	// 죽은 것으로 보고 끊는다. 연결 전체의 수명 제한이 아니다.
	writeTimeout = 5 * time.Second

	// 종료할 때 루프에 "나간다"를 전하는 데 쓰는 시간.
	leaveTimeout = 2 * time.Second

	defaultName = "guest"
)

// 클라이언트로 나가는 메시지. type으로 갈라 읽는다.
type welcomeMessage struct {
	Type     string          `json:"type"`
	You      game.PlayerID   `json:"you"`
	TickMS   int64           `json:"tickMs"`
	Stations []graph.Station `json:"stations"`
	Edges    []graph.Edge    `json:"edges"`
}

// Snapshot을 묻어 넣으면 JSON에서 필드가 평평하게 펼쳐진다.
// {"type":"snapshot","tick":1,"players":[...]}
type snapshotMessage struct {
	Type string `json:"type"`
	game.Snapshot
}

// 클라이언트에서 들어오는 메시지. M1에서 할 수 있는 것은 방향 전환 하나다.
type clientMessage struct {
	Type string `json:"type"`
}

type Hub struct {
	loop  *game.Loop
	graph *graph.Graph
	tick  time.Duration
}

func New(loop *game.Loop, g *graph.Graph, tick time.Duration) *Hub {
	return &Hub{loop: loop, graph: g, tick: tick}
}

// Handler는 /ws 핸들러를 만든다.
//
// ctx는 서버 전체의 수명이다. r.Context()를 쓰지 않는 이유는 http.Server.Shutdown이
// 이미 열려 있는 연결의 요청 컨텍스트를 취소하지 않기 때문이다. 그대로 두면
// 게임 접속이 하나라도 살아 있는 한 Shutdown이 타임아웃까지 기다린다.
func (h *Hub) Handler(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			// 기본값은 same-origin만 허용한다. 배포에서는 클라이언트를 같은
			// 오리진에서 내려주므로 그대로 통과하지만, 개발에서는 Vite(:5173)가
			// :8080으로 프록시하면서 Origin과 Host가 갈린다.
			OriginPatterns: []string{"localhost:*", "127.0.0.1:*"},
		})
		if err != nil {
			slog.Warn("ws: accept failed", "err", err)
			return
		}
		// 정상 종료 경로에서는 아래에서 이미 닫혔다. 여기 남긴 것은
		// 중간에 빠져나갈 때 소켓이 새지 않게 하는 보험이다.
		defer conn.CloseNow() //nolint:errcheck

		name := r.URL.Query().Get("name")
		if name == "" {
			name = defaultName
		}

		h.serve(ctx, conn, name)
	}
}

func (h *Hub) serve(ctx context.Context, conn *websocket.Conn, name string) {
	sess, err := h.loop.Join(ctx, name)
	if err != nil {
		slog.Warn("ws: join failed", "err", err)
		return
	}

	defer func() {
		// ctx가 이미 취소됐어도 나간다는 사실은 전해야 한다.
		// WithoutCancel로 취소만 떼어내고 시간 제한을 새로 건다.
		leaveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), leaveTimeout)
		defer cancel()
		if err := h.loop.Leave(leaveCtx, sess.ID); err != nil {
			slog.Warn("ws: leave failed", "player", sess.ID, "err", err)
		}
	}()

	// 지도는 한 번만 보낸다. 이후로는 스냅샷에 역 인덱스만 담기므로
	// 매 틱 오가는 양이 플레이어 수에만 비례한다.
	welcome := welcomeMessage{
		Type:     "welcome",
		You:      sess.ID,
		TickMS:   h.tick.Milliseconds(),
		Stations: h.graph.Stations(),
		Edges:    h.graph.Edges(),
	}
	if err := writeJSON(ctx, conn, welcome); err != nil {
		slog.Warn("ws: welcome failed", "player", sess.ID, "err", err)
		return
	}

	// 한쪽이 끝나면 다른 쪽도 깨운다. 클라이언트가 끊으면 읽기가 먼저 죽고,
	// 게임이 끝나면 쓰기가 먼저 죽는다 — 어느 쪽이든 둘 다 정리돼야 한다.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		defer cancel()
		h.readLoop(ctx, conn, sess.ID)
	}()

	go func() {
		defer wg.Done()
		defer cancel()
		h.writeLoop(ctx, conn, sess.Snapshots)
	}()

	wg.Wait()
	slog.Info("ws: disconnected", "player", sess.ID, "name", name)
}

func (h *Hub) readLoop(ctx context.Context, conn *websocket.Conn, id game.PlayerID) {
	for {
		var msg clientMessage
		// ctx 취소, 클라이언트 종료, 깨진 프레임이 모두 에러로 온다.
		// 어느 쪽이든 이 연결은 끝이다.
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			return
		}

		switch msg.Type {
		case "reverse":
			if err := h.loop.Reverse(ctx, id); err != nil {
				return
			}
		default:
			slog.Debug("ws: unknown message", "type", msg.Type, "player", id)
		}
	}
}

func (h *Hub) writeLoop(ctx context.Context, conn *websocket.Conn, snapshots <-chan game.Snapshot) {
	for {
		select {
		case <-ctx.Done():
			return

		case snap, ok := <-snapshots:
			if !ok {
				// 루프가 채널을 닫았다 — 게임이 끝났거나 이 플레이어가 지워졌다.
				conn.Close(websocket.StatusNormalClosure, "session ended") //nolint:errcheck
				return
			}
			if err := writeJSON(ctx, conn, snapshotMessage{Type: "snapshot", Snapshot: snap}); err != nil {
				return
			}
		}
	}
}

func writeJSON(ctx context.Context, conn *websocket.Conn, v any) error {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return wsjson.Write(ctx, conn, v)
}
