import { describe, expect, it } from 'vitest';

import type { Vec2 } from '@/graph/ring';
import type { Move, PlayerState, SnapshotMessage } from '@/net/protocol';

import { interpolate, movePosition } from './interpolate';

// 계산을 눈으로 따라갈 수 있게 직선으로 놓는다. A—B는 길이 3, B—C는 길이 3이다.
const stations: Vec2[] = [
  { x: 0, y: 0 }, // A
  { x: 3, y: 0 }, // B
  { x: 3, y: 3 }, // C
];

const [A, B, C] = [0, 1, 2];

function move(from: number, to: number, progress: number): Move {
  return { from, to, progress, totalTicks: 3 };
}

// 보간은 소유 현황을 보지 않는다. 비워 둔다.
function snapshot(tick: number, ...players: PlayerState[]): SnapshotMessage {
  return { type: 'snapshot', tick, players, edgeOwners: [], stationOwners: [] };
}

function player(id: number, m: Move): PlayerState {
  return { id, name: `p${id}`, move: m };
}

function expectPoint(actual: Vec2 | undefined, want: Vec2) {
  expect(actual).toBeDefined();
  expect(actual?.x).toBeCloseTo(want.x);
  expect(actual?.y).toBeCloseTo(want.y);
}

describe('movePosition', () => {
  it('progress 0은 출발역, totalTicks는 도착역', () => {
    expectPoint(movePosition(move(A, B, 0), stations) ?? undefined, { x: 0, y: 0 });
    expectPoint(movePosition(move(A, B, 3), stations) ?? undefined, { x: 3, y: 0 });
  });

  it('중간 지점은 비율대로 나눈다', () => {
    expectPoint(movePosition(move(A, B, 2), stations) ?? undefined, { x: 2, y: 0 });
  });

  it('지도에 없는 역은 null', () => {
    expect(movePosition(move(A, 99, 1), stations)).toBeNull();
  });
});

describe('interpolate', () => {
  it('같은 구간 안에서는 두 지점 사이를 훑는다', () => {
    const previous = snapshot(1, player(1, move(A, B, 1))); // (1,0)
    const latest = snapshot(2, player(1, move(A, B, 2))); // (2,0)

    expectPoint(interpolate(previous, latest, 0, stations)[0]?.position, { x: 1, y: 0 });
    expectPoint(interpolate(previous, latest, 0.5, stations)[0]?.position, { x: 1.5, y: 0 });
    expectPoint(interpolate(previous, latest, 1, stations)[0]?.position, { x: 2, y: 0 });
  });

  // 여기가 월드 좌표로 섞는 이유다. 격자 값으로 섞었다면 progress 2에서 0으로
  // 떨어지며 플레이어가 역으로 되감겼을 것이다.
  it('구간을 갈아타도 지나온 직선 위에 머문다', () => {
    const previous = snapshot(1, player(1, move(A, B, 2))); // (2,0)
    const latest = snapshot(2, player(1, move(B, C, 0))); // (3,0) = B역

    expectPoint(interpolate(previous, latest, 0, stations)[0]?.position, { x: 2, y: 0 });
    expectPoint(interpolate(previous, latest, 0.5, stations)[0]?.position, { x: 2.5, y: 0 });
    expectPoint(interpolate(previous, latest, 1, stations)[0]?.position, { x: 3, y: 0 });
  });

  // 서버가 방향을 뒤집을 때 progress도 함께 뒤집는다. 그래서 뒤집기 전후의
  // 월드 좌표가 같고, 화면에서는 위치가 전혀 튀지 않아야 한다.
  it('방향 전환은 위치를 흔들지 않는다', () => {
    const previous = snapshot(1, player(1, move(A, B, 1))); // (1,0)
    const latest = snapshot(2, player(1, move(B, A, 2))); // 3 + (0-3)*2/3 = (1,0)

    for (const alpha of [0, 0.25, 0.5, 0.75, 1]) {
      expectPoint(interpolate(previous, latest, alpha, stations)[0]?.position, { x: 1, y: 0 });
    }
  });

  it('직전 틱에 없던 플레이어는 최신 위치에 바로 놓는다', () => {
    const previous = snapshot(1);
    const latest = snapshot(2, player(7, move(A, B, 2)));

    expectPoint(interpolate(previous, latest, 0, stations)[0]?.position, { x: 2, y: 0 });
  });

  it('직전 스냅샷이 아예 없어도 그린다', () => {
    const latest = snapshot(1, player(1, move(A, B, 1)));

    expectPoint(interpolate(null, latest, 0.5, stations)[0]?.position, { x: 1, y: 0 });
  });

  it('나간 플레이어는 그리지 않는다', () => {
    const previous = snapshot(1, player(1, move(A, B, 1)), player(2, move(A, B, 2)));
    const latest = snapshot(2, player(1, move(A, B, 2)));

    const rendered = interpolate(previous, latest, 1, stations);
    expect(rendered.map((p) => p.id)).toEqual([1]);
  });

  it('alpha가 범위를 벗어나면 잘라낸다', () => {
    const previous = snapshot(1, player(1, move(A, B, 0)));
    const latest = snapshot(2, player(1, move(A, B, 3)));

    expectPoint(interpolate(previous, latest, -5, stations)[0]?.position, { x: 0, y: 0 });
    expectPoint(interpolate(previous, latest, 5, stations)[0]?.position, { x: 3, y: 0 });
    expectPoint(interpolate(previous, latest, Number.NaN, stations)[0]?.position, { x: 0, y: 0 });
  });
});
