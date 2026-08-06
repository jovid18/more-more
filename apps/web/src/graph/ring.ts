/**
 * 원 위에 `count`개의 점을 고르게 놓고, x·y·z를 이어붙인 배열로 돌려준다.
 *
 * 첫 점은 `(radius, 0, 0)`이고 반시계 방향으로 돈다. z는 전부 0이다 — 지도는 평면이고,
 * 3D는 나중에 노선을 층으로 분리할 때 쓴다.
 *
 * 야마노테선 30역의 자리를 잡아두는 임시 배치다. 실제 역 좌표가 들어오면 통째로 교체된다.
 */
export function ringPositions(count: number, radius: number): Float32Array {
  if (!Number.isInteger(count) || count < 1) {
    throw new RangeError(`count must be a positive integer, got ${count}`);
  }

  const positions = new Float32Array(count * 3);

  for (let i = 0; i < count; i++) {
    const angle = (i / count) * Math.PI * 2;
    positions[i * 3] = Math.cos(angle) * radius;
    positions[i * 3 + 1] = Math.sin(angle) * radius;
  }

  return positions;
}
