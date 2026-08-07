import { describe, expect, it } from 'vitest';

import { parseServerMessage } from './protocol';

const welcome = {
  type: 'welcome',
  you: 1,
  tickMs: 500,
  stations: [
    { id: 0, name: '東京', romaji: 'Tokyo' },
    { id: 1, name: '有楽町', romaji: 'Yurakucho' },
  ],
  edges: [{ a: 0, b: 1, travelTicks: 3 }],
};

const snapshot = {
  type: 'snapshot',
  tick: 42,
  players: [{ id: 1, name: 'hana', move: { from: 0, to: 1, progress: 1, totalTicks: 3 }, score: 16, away: false }],
  edgeOwners: [1],
  stationOwners: [0, 1],
  phase: 'playing',
  ticksLeft: 558,
  winner: 0,
};

/** 필드 하나를 바꾼 사본. 원본을 건드리지 않고 실패 사례를 만든다. */
function withField(base: object, patch: object): string {
  return JSON.stringify({ ...base, ...patch });
}

describe('parseServerMessage', () => {
  it('welcome을 읽는다', () => {
    const parsed = parseServerMessage(JSON.stringify(welcome));

    expect(parsed?.type).toBe('welcome');
    if (parsed?.type !== 'welcome') {
      return;
    }
    expect(parsed.you).toBe(1);
    expect(parsed.tickMs).toBe(500);
    expect(parsed.stations[1]?.name).toBe('有楽町');
    expect(parsed.edges[0]?.travelTicks).toBe(3);
  });

  it('snapshot을 읽는다', () => {
    const parsed = parseServerMessage(JSON.stringify(snapshot));

    expect(parsed?.type).toBe('snapshot');
    if (parsed?.type !== 'snapshot') {
      return;
    }
    expect(parsed.tick).toBe(42);
    expect(parsed.players[0]?.move.totalTicks).toBe(3);
    expect(parsed.players[0]?.score).toBe(16);
    expect(parsed.players[0]?.away).toBe(false);
    expect(parsed.edgeOwners).toEqual([1]);
    expect(parsed.stationOwners).toEqual([0, 1]);
    expect(parsed.phase).toBe('playing');
    expect(parsed.ticksLeft).toBe(558);
    expect(parsed.winner).toBe(0);
  });

  // away는 없으면 false다. 나머지 필드와 달리 이건 옛 서버와도 맞물려 돌아야 한다.
  it('away가 빠져 있으면 접속 중으로 본다', () => {
    const players = [{ id: 1, name: 'hana', move: { from: 0, to: 1, progress: 1, totalTicks: 3 }, score: 0 }];
    const parsed = parseServerMessage(withField(snapshot, { players }));

    expect(parsed?.type).toBe('snapshot');
    if (parsed?.type !== 'snapshot') {
      return;
    }
    expect(parsed.players[0]?.away).toBe(false);
  });

  it('모르는 phase는 버린다', () => {
    expect(parseServerMessage(withField(snapshot, { phase: 'paused' }))).toBeNull();
    expect(parseServerMessage(withField(snapshot, { phase: undefined }))).toBeNull();
  });

  // travelTicks와 totalTicks는 둘 다 나눗셈의 분모로 쓰인다. 0이 흘러들면
  // 위치가 Infinity나 NaN이 되고, 화면에서는 말이 사라진 것처럼 보인다.
  it('구간 길이가 0 이하면 메시지를 버린다', () => {
    for (const travelTicks of [0, -1]) {
      expect(parseServerMessage(withField(welcome, { edges: [{ a: 0, b: 1, travelTicks }] }))).toBeNull();
    }

    expect(parseServerMessage(withField(welcome, { edges: [{ a: 0, b: 1 }] }))).toBeNull();
  });

  it('move의 totalTicks가 0 이하면 버린다', () => {
    const players = [{ id: 1, name: 'hana', move: { from: 0, to: 1, progress: 1, totalTicks: 0 } }];
    expect(parseServerMessage(withField(snapshot, { players }))).toBeNull();
  });

  it('소유 배열이 숫자 배열이 아니면 버린다', () => {
    expect(parseServerMessage(withField(snapshot, { edgeOwners: ['1'] }))).toBeNull();
    expect(parseServerMessage(withField(snapshot, { stationOwners: null }))).toBeNull();
  });

  it('모르는 type과 깨진 JSON은 버린다', () => {
    expect(parseServerMessage('{"type":"something-new"}')).toBeNull();
    expect(parseServerMessage('{ not json')).toBeNull();
    expect(parseServerMessage('[]')).toBeNull();
  });
});
