import { useCallback, useEffect, useRef, useState } from 'react';

import type { Vec2 } from '@/graph/ring';
import { ringLayout } from '@/graph/ring';
import type { Connection, ConnectionStatus } from '@/net/connection';
import { connect, gameSocketURL } from '@/net/connection';
import type { Edge, PlayerId, SnapshotMessage } from '@/net/protocol';
import { interpolate } from '@/render/interpolate';
import type { Frame } from '@/render/scene';
import { mountScene } from '@/render/scene';

const mapRadius = 2.4;

// 참조가 고정돼야 한다. 장면은 stations의 참조가 바뀔 때만 지도를 다시 만드는데,
// 매번 새 빈 배열을 넘기면 연결되기 전까지 매 프레임 다시 만든다.
const noStations: readonly Vec2[] = [];
const noEdges: readonly Edge[] = [];
const emptyFrame: Frame = { stations: noStations, edges: noEdges, players: [], you: null };

interface GameState {
  stations: readonly Vec2[];
  edges: readonly Edge[];
  you: PlayerId | null;
  tickMs: number;
  previous: SnapshotMessage | null;
  latest: SnapshotMessage | null;
  /** latest가 도착한 시각. 보간의 기준점이다. */
  latestAt: number;
}

interface Hud {
  status: ConnectionStatus;
  tick: number;
  players: number;
}

export function App() {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const connectionRef = useRef<Connection | null>(null);

  // 그림에 쓰는 값은 ref에 둔다. 초당 두 번 오는 스냅샷마다 React를 다시 그리게
  // 하면 60fps 렌더 루프와 무관한 일로 프레임을 갉아먹는다.
  const stateRef = useRef<GameState>({
    stations: noStations,
    edges: noEdges,
    you: null,
    tickMs: 500,
    previous: null,
    latest: null,
    latestAt: 0,
  });

  // 화면에 글자로 나가는 것만 React 상태다.
  const [hud, setHud] = useState<Hud>({ status: 'connecting', tick: 0, players: 0 });
  const [name] = useState(() => `runner-${Math.floor(Math.random() * 900 + 100)}`);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) {
      return undefined;
    }

    return mountScene(canvas, () => {
      const state = stateRef.current;
      if (!state.latest || state.stations.length === 0) {
        return emptyFrame;
      }

      // 마지막 스냅샷이 온 뒤 흐른 시간을 틱 길이로 나눈다. 1을 넘으면
      // (서버가 늦거나 끊겼으면) 최신 위치에 멈춰 선다.
      const alpha = (performance.now() - state.latestAt) / state.tickMs;

      return {
        stations: state.stations,
        edges: state.edges,
        players: interpolate(state.previous, state.latest, alpha, state.stations),
        you: state.you,
      };
    });
  }, []);

  useEffect(() => {
    const connection = connect(gameSocketURL(name), {
      onStatus(status) {
        setHud((current) => ({ ...current, status }));
      },
      onMessage(message) {
        const state = stateRef.current;

        if (message.type === 'welcome') {
          // 실제 위경도가 아직 없으므로 역 수만큼 원형으로 놓는다.
          // 이 배열의 참조가 곧 "지도가 바뀌었다"는 신호다.
          state.stations = ringLayout(message.stations.length, mapRadius);
          state.edges = message.edges;
          state.you = message.you;
          state.tickMs = message.tickMs;
          // 지도가 갈렸으므로 예전 위치로 보간하지 않는다.
          state.previous = null;
          state.latest = null;
          return;
        }

        state.previous = state.latest;
        state.latest = message;
        state.latestAt = performance.now();

        setHud((current) => ({ ...current, tick: message.tick, players: message.players.length }));
      },
    });

    connectionRef.current = connection;

    return () => {
      connection.close();
      connectionRef.current = null;
    };
  }, [name]);

  const reverse = useCallback(() => {
    connectionRef.current?.send({ type: 'reverse' });
  }, []);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.code === 'Space') {
        // 스페이스는 기본적으로 페이지를 스크롤한다.
        event.preventDefault();
        reverse();
      }
    };

    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [reverse]);

  return (
    <main className="stage">
      <canvas ref={canvasRef} className="canvas" />

      <div className="hud">
        <span className={`status status-${hud.status}`}>{statusLabel[hud.status]}</span>
        <span className="name">{name}</span>
        <dl>
          <dt>tick</dt>
          <dd>{hud.tick}</dd>
          <dt>players</dt>
          <dd>{hud.players}</dd>
        </dl>
      </div>

      <button type="button" className="reverse" onClick={reverse}>
        방향 전환 <kbd>Space</kbd>
      </button>
    </main>
  );
}

const statusLabel: Record<ConnectionStatus, string> = {
  connecting: '연결 중',
  open: '연결됨',
  closed: '끊김 · 재시도 중',
};
