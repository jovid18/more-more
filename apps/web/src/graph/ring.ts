export interface Vec2 {
  x: number;
  y: number;
}

/**
 * 원 위에 `count`개의 점을 고르게 놓는다.
 *
 * 첫 점은 `(radius, 0)`이고 반시계 방향으로 돈다. 지도는 평면이라 z는 없다 —
 * 3D는 나중에 노선을 층으로 분리할 때 쓴다.
 *
 * 야마노테선 30역의 자리를 잡아두는 임시 배치다. 실제 위경도가 들어오면
 * 이 함수를 갈아끼우면 되고, 그 위의 렌더링과 보간은 그대로 쓴다.
 */
export function ringLayout(count: number, radius: number): Vec2[] {
  if (!Number.isInteger(count) || count < 1) {
    throw new RangeError(`count must be a positive integer, got ${count}`);
  }

  const points: Vec2[] = [];
  for (let i = 0; i < count; i++) {
    const angle = (i / count) * Math.PI * 2;
    points.push({ x: Math.cos(angle) * radius, y: Math.sin(angle) * radius });
  }

  return points;
}

/** three.js의 BufferAttribute가 먹는 평평한 배열로 편다. z는 0으로 채운다. */
export function toPositionArray(points: readonly Vec2[]): Float32Array {
  const positions = new Float32Array(points.length * 3);

  for (const [i, point] of points.entries()) {
    positions[i * 3] = point.x;
    positions[i * 3 + 1] = point.y;
  }

  return positions;
}
