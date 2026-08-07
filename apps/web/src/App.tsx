import { useCallback, useEffect, useRef, useState } from 'react';

import type { Vec2 } from '@/graph/ring';
import { ringLayout } from '@/graph/ring';
import type { Connection, ConnectionStatus } from '@/net/connection';
import { connect, gameSocketURL } from '@/net/connection';
import type { Edge, Phase, PlayerId, PlayerState, SnapshotMessage } from '@/net/protocol';

import { interpolate } from '@/render/interpolate';
import type { Frame } from '@/render/scene';
import { hueFor, mountScene } from '@/render/scene';

const mapRadius = 2.4;

// 참조가 고정돼야 한다. 장면은 stations의 참조가 바뀔 때만 지도를 다시 만드는데,
// 매번 새 빈 배열을 넘기면 연결되기 전까지 매 프레임 다시 만든다.
const noStations: readonly Vec2[] = [];
const noEdges: readonly Edge[] = [];
const noOwners: readonly PlayerId[] = [];
const emptyFrame: Frame = {
  stations: noStations,
  edges: noEdges,
  players: [],
  you: null,
  edgeOwners: noOwners,
  stationOwners: noOwners,
};

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
  phase: Phase;
  /** 지금 국면이 끝나기까지 남은 초. */
  secondsLeft: number;
  score: number;
  /** 내가 가진 구간과 역. 점수가 어디서 나오는지를 나눠 본다. */
  edges: number;
  stations: number;
  /** 점수 순으로 줄 세운 전원. 몇 위인지가 이 게임의 유일한 목표다. */
  board: PlayerState[];
  winner: PlayerId;
  you: PlayerId | null;
}

const initialHud: Hud = {
  status: 'connecting',
  phase: 'playing',
  secondsLeft: 0,
  score: 0,
  edges: 0,
  stations: 0,
  board: [],
  winner: 0,
  you: null,
};

function formatClock(seconds: number): string {
  const minutes = Math.floor(seconds / 60);
  return `${minutes}:${String(seconds % 60).padStart(2, '0')}`;
}

/** 장면과 같은 계산을 써야 순위표의 색과 지도 위의 말이 일치한다. */
function colorOf(id: PlayerId): string {
  return `hsl(${hueFor(id) * 360}deg 70% 62%)`;
}

function resultText(hud: Hud): string {
  if (hud.winner === 0) {
    return '무승부';
  }

  const champion = hud.board.find((player) => player.id === hud.winner);
  if (!champion) {
    return '판 종료';
  }
  return hud.winner === hud.you ? `승리 · ${champion.score}점` : `${champion.name} 승리 · ${champion.score}점`;
}

function countOwnedBy(owners: readonly PlayerId[], you: PlayerId | null): number {
  if (you === null) {
    return 0;
  }

  let owned = 0;
  for (const owner of owners) {
    if (owner === you) {
      owned++;
    }
  }
  return owned;
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
  const [hud, setHud] = useState<Hud>(initialHud);
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
        // 소유는 보간하지 않는다. 한 틱에 딱 바뀌는 값이라 섞으면 색만 흐려진다.
        edgeOwners: state.latest.edgeOwners,
        stationOwners: state.latest.stationOwners,
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
          setHud((current) => ({ ...current, you: message.you }));
          return;
        }

        // 지도와 크기가 맞지 않으면 버린다. 서버와 버전이 갈리면 소유 배열이
        // 짧게 올 수 있는데, 그대로 그리면 마지막 구간들이 조용히 주인 없는
        // 색으로 남아 원인을 찾기 어렵다.
        if (
          message.edgeOwners.length !== state.edges.length ||
          message.stationOwners.length !== state.stations.length
        ) {
          console.warn(
            `snapshot dropped: ${message.edgeOwners.length}/${message.stationOwners.length} owners ` +
              `for a map of ${state.edges.length}/${state.stations.length}`,
          );
          return;
        }

        state.previous = state.latest;
        state.latest = message;
        state.latestAt = performance.now();

        const board = message.players.toSorted((a, b) => b.score - a.score);
        const you = message.players.find((player) => player.id === state.you);

        setHud((current) => ({
          ...current,
          phase: message.phase,
          secondsLeft: Math.ceil((message.ticksLeft * state.tickMs) / 1000),
          score: you?.score ?? 0,
          edges: countOwnedBy(message.edgeOwners, state.you),
          stations: countOwnedBy(message.stationOwners, state.you),
          board,
          winner: message.winner,
        }));
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

        <div className="clock">
          <span>{hud.phase === 'playing' ? '남은 시간' : '다음 판까지'}</span>
          <time>{formatClock(hud.secondsLeft)}</time>
        </div>

        <dl>
          <dt>내 점수</dt>
          <dd>{hud.score}</dd>
          <dt>구간</dt>
          <dd>{hud.edges}</dd>
          <dt>역</dt>
          <dd>{hud.stations}</dd>
        </dl>

        {hud.board.length > 0 && (
          <ol className="board">
            {hud.board.map((player) => (
              <li key={player.id} className={player.id === hud.you ? 'me' : undefined}>
                <span className="chip" style={{ background: colorOf(player.id) }} />
                <span className="who">{player.name}</span>
                <span className="pts">{player.score}</span>
              </li>
            ))}
          </ol>
        )}
      </div>

      {hud.phase === 'finished' && (
        <div className="result" role="status">
          <strong>{resultText(hud)}</strong>
          <span>다음 판까지 {hud.secondsLeft}초</span>
        </div>
      )}

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
