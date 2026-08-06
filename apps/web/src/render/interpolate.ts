import type { Vec2 } from '@/graph/ring';
import type { Move, PlayerId, SnapshotMessage } from '@/net/protocol';

export interface RenderedPlayer {
  id: PlayerId;
  name: string;
  position: Vec2;
}

/**
 * 구간 위 어디쯤인지를 월드 좌표로 바꾼다.
 * 역 인덱스가 지도 밖이면 null — 서버와 지도 버전이 갈린 경우다.
 */
export function movePosition(move: Move, stations: readonly Vec2[]): Vec2 | null {
  const from = stations[move.from];
  const to = stations[move.to];
  if (!from || !to) {
    return null;
  }

  const t = clamp01(move.progress / move.needTicks);
  return { x: from.x + (to.x - from.x) * t, y: from.y + (to.y - from.y) * t };
}

/**
 * 스냅샷 두 장 사이를 alpha(0~1)로 섞어 그릴 위치를 낸다.
 *
 * 서버는 초당 2번만 말하므로 그대로 그리면 화면이 뚝뚝 끊긴다. 직전 틱과 최신 틱의
 * 위치를 구해 그 사이를 시간에 맞춰 훑으면 60fps로 부드러워진다. 대신 화면은
 * 항상 한 틱만큼 과거다 — 실시간성보다 매끄러움을 택한 것이고, 판정은 어차피
 * 서버가 하므로 잃는 것이 없다.
 *
 * **격자가 아니라 월드 좌표에서 섞는 것이 요점이다.** 구간을 갈아타는 순간에도
 * 그냥 맞는다: 직전이 A→B의 2/3 지점이고 최신이 B→C의 0 지점(=B역)이면, 두 점을
 * 잇는 직선은 A→B의 남은 1/3과 정확히 겹친다.
 *
 * 방향 전환도 마찬가지다. 서버가 뒤집을 때 progress까지 함께 뒤집어 주므로
 * 뒤집기 전후의 월드 좌표가 같고, 그래서 화면이 튀지 않는다.
 */
export function interpolate(
  previous: SnapshotMessage | null,
  latest: SnapshotMessage,
  alpha: number,
  stations: readonly Vec2[],
): RenderedPlayer[] {
  const t = clamp01(alpha);

  const before = new Map<PlayerId, Move>();
  if (previous) {
    for (const player of previous.players) {
      before.set(player.id, player.move);
    }
  }

  const rendered: RenderedPlayer[] = [];

  for (const player of latest.players) {
    const target = movePosition(player.move, stations);
    if (!target) {
      continue;
    }

    // 직전 틱에 없던 플레이어는 섞을 것이 없다. 방금 접속한 사람이
    // 지도 원점에서 날아오지 않게 최신 위치에 바로 놓는다.
    const priorMove = before.get(player.id);
    const start = priorMove ? movePosition(priorMove, stations) : null;

    rendered.push({
      id: player.id,
      name: player.name,
      position: start ? { x: start.x + (target.x - start.x) * t, y: start.y + (target.y - start.y) * t } : target,
    });
  }

  return rendered;
}

function clamp01(value: number): number {
  if (!Number.isFinite(value)) {
    return 0;
  }
  return Math.min(Math.max(value, 0), 1);
}
