/**
 * 서버와 주고받는 메시지의 형태.
 *
 * Go 쪽 `internal/hub`의 구조체와 짝이 맞아야 한다. 한쪽만 바꾸면 컴파일은
 * 통과하고 런타임에 조용히 어긋나므로, 여기 손대면 반대편도 같이 볼 것.
 * 두 언어를 잇는 스키마는 아직 없다 — 메시지가 늘면 그때 만든다.
 */

export type StationId = number;
export type PlayerId = number;

export interface Station {
  id: StationId;
  name: string;
  romaji: string;
}

export interface Edge {
  a: StationId;
  b: StationId;

  /**
   * 이 구간을 건너는 데 걸리는 틱 수. 좌표가 없으므로 이것이 구간의 길이다.
   * 점수 가중치가 여기 붙는다.
   */
  travelTicks: number;
}

/**
 * 좌표가 없다. 서버는 "totalTicks 중 progress번째"까지만 알고,
 * 그 사이를 메우는 것은 클라이언트의 보간이다.
 *
 * totalTicks는 그 구간의 `Edge.travelTicks`를 베껴 온 값이다. 지도를 다시 뒤지지
 * 않아도 되게 실려 오고, 나중에 플레이어별 속도 보정이 생기면 둘이 갈라진다.
 */
export interface Move {
  from: StationId;
  to: StationId;
  progress: number;
  totalTicks: number;
}

export interface PlayerState {
  id: PlayerId;
  name: string;
  move: Move;
  score: number;

  /** 연결이 끊겨 유예 시간 안에 있다. 그 자리에 멈춰 있고 땅은 그대로다. */
  away: boolean;
}

/** 판이 진행 중인지, 결과를 보여주는 중인지. */
export type Phase = 'playing' | 'finished';

export interface WelcomeMessage {
  type: 'welcome';
  you: PlayerId;
  tickMs: number;
  stations: Station[];
  edges: Edge[];
}

export interface SnapshotMessage {
  type: 'snapshot';
  tick: number;
  players: PlayerState[];

  /**
   * 인덱스는 구간·역 번호, 값은 주인의 PlayerId다. 0은 주인 없음(PlayerId는 1부터).
   * 구간 번호는 welcome의 `edges` 순서와 같다.
   *
   * welcome의 `edges`와 이름을 나눈 것은 의도적이다 — 한쪽은 지도의 구간 목록이고
   * 이쪽은 그 구간의 주인이라, 같은 키를 쓰면 조용히 잘못 읽힌다.
   */
  edgeOwners: PlayerId[];
  stationOwners: PlayerId[];

  phase: Phase;

  /** 지금 국면이 끝나기까지 남은 틱. playing이면 판의 남은 시간, finished면 다음 판까지. */
  ticksLeft: number;

  /** 끝난 판의 승자. 0은 진행 중이거나 무승부다. */
  winner: PlayerId;
}

export type ServerMessage = WelcomeMessage | SnapshotMessage;

export type ClientMessage = { type: 'reverse' };

/**
 * 네트워크로 들어온 문자열을 검사해서 메시지로 만든다. 모양이 어긋나면 null이다.
 *
 * 서버가 보냈다는 이유로 믿지 않는다 — 배포 중 버전이 갈리면 필드가 빠진 채로
 * 올 수 있고, 그때 화면이 깨지는 것보다 그 메시지 하나를 버리는 편이 낫다.
 */
export function parseServerMessage(raw: string): ServerMessage | null {
  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch {
    return null;
  }

  if (!isRecord(value)) {
    return null;
  }

  switch (value['type']) {
    case 'welcome':
      return parseWelcome(value);
    case 'snapshot':
      return parseSnapshot(value);
    default:
      return null;
  }
}

function parseWelcome(value: Record<string, unknown>): WelcomeMessage | null {
  const you = value['you'];
  const tickMs = value['tickMs'];
  const stations = value['stations'];
  const edges = value['edges'];

  if (typeof you !== 'number' || typeof tickMs !== 'number' || !Array.isArray(stations) || !Array.isArray(edges)) {
    return null;
  }

  const parsedStations: Station[] = [];
  for (const entry of stations) {
    if (!isRecord(entry) || typeof entry['id'] !== 'number') {
      return null;
    }
    parsedStations.push({
      id: entry['id'],
      name: typeof entry['name'] === 'string' ? entry['name'] : '',
      romaji: typeof entry['romaji'] === 'string' ? entry['romaji'] : '',
    });
  }

  const parsedEdges: Edge[] = [];
  for (const entry of edges) {
    if (!isRecord(entry) || typeof entry['a'] !== 'number' || typeof entry['b'] !== 'number') {
      return null;
    }
    // 0이나 음수면 서버가 나눗셈의 분모로 쓰지 못할 값을 보낸 것이다.
    const travelTicks = entry['travelTicks'];
    if (typeof travelTicks !== 'number' || travelTicks <= 0) {
      return null;
    }
    parsedEdges.push({ a: entry['a'], b: entry['b'], travelTicks });
  }

  return { type: 'welcome', you, tickMs, stations: parsedStations, edges: parsedEdges };
}

function parseSnapshot(value: Record<string, unknown>): SnapshotMessage | null {
  const tick = value['tick'];
  const players = value['players'];

  if (typeof tick !== 'number' || !Array.isArray(players)) {
    return null;
  }

  const edgeOwners = parseOwners(value['edgeOwners']);
  const stationOwners = parseOwners(value['stationOwners']);
  if (!edgeOwners || !stationOwners) {
    return null;
  }

  const parsed: PlayerState[] = [];
  for (const entry of players) {
    if (!isRecord(entry) || typeof entry['id'] !== 'number') {
      return null;
    }
    const move = parseMove(entry['move']);
    if (!move) {
      return null;
    }
    parsed.push({
      id: entry['id'],
      name: typeof entry['name'] === 'string' ? entry['name'] : '',
      move,
      score: typeof entry['score'] === 'number' ? entry['score'] : 0,
      away: entry['away'] === true,
    });
  }

  const phase = value['phase'];
  if (phase !== 'playing' && phase !== 'finished') {
    return null;
  }

  const ticksLeft = value['ticksLeft'];
  const winner = value['winner'];
  if (typeof ticksLeft !== 'number' || typeof winner !== 'number') {
    return null;
  }

  return { type: 'snapshot', tick, players: parsed, edgeOwners, stationOwners, phase, ticksLeft, winner };
}

function parseOwners(value: unknown): PlayerId[] | null {
  if (!Array.isArray(value)) {
    return null;
  }

  const owners: PlayerId[] = [];
  for (const entry of value) {
    if (typeof entry !== 'number') {
      return null;
    }
    owners.push(entry);
  }
  return owners;
}

function parseMove(value: unknown): Move | null {
  if (!isRecord(value)) {
    return null;
  }

  const from = value['from'];
  const to = value['to'];
  const progress = value['progress'];
  const totalTicks = value['totalTicks'];

  if (
    typeof from !== 'number' ||
    typeof to !== 'number' ||
    typeof progress !== 'number' ||
    typeof totalTicks !== 'number' ||
    totalTicks <= 0
  ) {
    return null;
  }

  return { from, to, progress, totalTicks };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
