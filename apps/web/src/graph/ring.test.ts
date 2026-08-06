import { describe, expect, it } from 'vitest';

import { ringLayout, toPositionArray } from './ring';

describe('ringLayout', () => {
  it('요청한 개수만큼 점을 낸다', () => {
    expect(ringLayout(30, 1)).toHaveLength(30);
  });

  it('첫 점은 +x 축 위에 있다', () => {
    const [first] = ringLayout(4, 2);
    expect(first?.x).toBeCloseTo(2);
    expect(first?.y).toBeCloseTo(0);
  });

  it('모든 점이 반지름 위에 놓인다', () => {
    const radius = 2.2;
    for (const point of ringLayout(30, radius)) {
      expect(Math.hypot(point.x, point.y)).toBeCloseTo(radius);
    }
  });

  it('반시계 방향으로 돈다', () => {
    const [, second] = ringLayout(4, 1);
    expect(second?.y).toBeGreaterThan(0);
  });

  it('count가 양의 정수가 아니면 거부한다', () => {
    expect(() => ringLayout(0, 1)).toThrow(RangeError);
    expect(() => ringLayout(2.5, 1)).toThrow(RangeError);
  });
});

describe('toPositionArray', () => {
  it('점 하나당 좌표 셋으로 펴고 z는 0으로 둔다', () => {
    const flat = toPositionArray([
      { x: 1, y: 2 },
      { x: 3, y: 4 },
    ]);

    expect(Array.from(flat)).toEqual([1, 2, 0, 3, 4, 0]);
  });
});
