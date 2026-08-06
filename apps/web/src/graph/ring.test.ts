import { describe, expect, it } from 'vitest';

import { ringPositions } from './ring';

/** noUncheckedIndexedAccess 아래서 TypedArray 인덱싱은 `number | undefined`다. */
function at(positions: Float32Array, index: number): number {
  const value = positions[index];
  if (value === undefined) {
    throw new RangeError(`index ${index} out of range`);
  }
  return value;
}

describe('ringPositions', () => {
  it('점 하나당 좌표 셋을 낸다', () => {
    expect(ringPositions(30, 1)).toHaveLength(90);
  });

  it('첫 점은 +x 축 위에 있다', () => {
    const positions = ringPositions(4, 2);
    expect(at(positions, 0)).toBeCloseTo(2);
    expect(at(positions, 1)).toBeCloseTo(0);
  });

  it('모든 점이 반지름 위에 놓인다', () => {
    const radius = 2.2;
    const count = 30;
    const positions = ringPositions(count, radius);

    for (let i = 0; i < count; i++) {
      const x = at(positions, i * 3);
      const y = at(positions, i * 3 + 1);
      expect(Math.hypot(x, y)).toBeCloseTo(radius);
      expect(at(positions, i * 3 + 2)).toBe(0);
    }
  });

  it('count가 양의 정수가 아니면 거부한다', () => {
    expect(() => ringPositions(0, 1)).toThrow(RangeError);
    expect(() => ringPositions(2.5, 1)).toThrow(RangeError);
  });
});
