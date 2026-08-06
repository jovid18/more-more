import {
  BufferAttribute,
  BufferGeometry,
  CanvasTexture,
  Color,
  Group,
  LineBasicMaterial,
  LineSegments,
  PerspectiveCamera,
  Points,
  PointsMaterial,
  Scene,
  SRGBColorSpace,
  WebGLRenderer,
} from 'three';

import type { Vec2 } from '@/graph/ring';
import { toPositionArray } from '@/graph/ring';
import type { Edge, PlayerId } from '@/net/protocol';

import type { RenderedPlayer } from './interpolate';

export interface Frame {
  /**
   * 지도. 참조가 바뀔 때만 기하를 다시 만든다 — 매 프레임 새 배열을 넘기면
   * 30역짜리 지도를 초당 60번 GPU로 다시 올리게 된다.
   */
  stations: readonly Vec2[];
  edges: readonly Edge[];
  players: readonly RenderedPlayer[];
  you: PlayerId | null;

  /** 인덱스 = 구간·역 번호, 값 = 주인. 0은 주인 없음. 이것도 참조로 변경을 가린다. */
  edgeOwners: readonly PlayerId[];
  stationOwners: readonly PlayerId[];
}

// 자리를 미리 잡아두고 그 안에서만 쓴다. 매 프레임 버퍼를 새로 만들면
// GC가 프레임 사이에 끼어들어 화면이 걸린다.
const maxPlayers = 256;

// 주인 없는 땅. 색을 완전히 죽이지 않는 것은 지도의 모양은 늘 보여야 하기 때문이다.
const unownedTrack = new Color('#28323f');
const unownedStation = new Color('#5a6b80');

/**
 * 캔버스에 지도와 플레이어를 그린다. 정리 함수를 돌려준다.
 *
 * 매 프레임 getFrame()을 불러 그릴 것을 받아온다. React 상태를 거치지 않는 것은
 * 초당 60번 리렌더를 시키지 않기 위해서다 — 그림은 여기서, 화면 문자열은 React에서.
 */
export function mountScene(canvas: HTMLCanvasElement, getFrame: () => Frame): () => void {
  const renderer = new WebGLRenderer({ canvas, antialias: true });
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));

  const scene = new Scene();
  scene.background = new Color('#0b0d12');

  // 거의 위에서 내려다본다. 지금 지도는 평면이라 크게 기울이면 원이 눌려 보이기만 한다.
  // 노선을 z축으로 층 분리할 때 이 각도를 다시 열면 된다.
  const camera = new PerspectiveCamera(50, 1, 0.1, 100);
  camera.position.set(0, -1.7, 6.4);
  camera.lookAt(0, 0, 0);

  const dot = createDotTexture();

  // ─── 지도 ────────────────────────────────────────────────
  const map = new Group();
  scene.add(map);

  const trackGeometry = new BufferGeometry();
  const track = new LineSegments(trackGeometry, new LineBasicMaterial({ vertexColors: true }));

  const stationGeometry = new BufferGeometry();
  const stationDots = new Points(stationGeometry, dotMaterial(dot, { size: 0.08, vertexColors: true }));

  map.add(track, stationDots);

  let drawnStations: readonly Vec2[] | null = null;
  let trackColors: Float32Array | null = null;
  let stationColors: Float32Array | null = null;

  const buildMap = (stations: readonly Vec2[], edges: readonly Edge[]) => {
    stationGeometry.dispose();
    stationGeometry.setAttribute('position', new BufferAttribute(toPositionArray(stations), 3));
    stationColors = new Float32Array(stations.length * 3);
    stationGeometry.setAttribute('color', new BufferAttribute(stationColors, 3));

    // LineSegments는 정점을 둘씩 짝지어 선분 하나로 본다. 구간마다 양 끝을 적는다.
    const segments = new Float32Array(edges.length * 6);
    let cursor = 0;
    for (const edge of edges) {
      const a = stations[edge.a];
      const b = stations[edge.b];
      if (!a || !b) {
        continue;
      }
      segments[cursor++] = a.x;
      segments[cursor++] = a.y;
      segments[cursor++] = 0;
      segments[cursor++] = b.x;
      segments[cursor++] = b.y;
      segments[cursor++] = 0;
    }

    trackGeometry.dispose();
    trackGeometry.setAttribute('position', new BufferAttribute(segments, 3));
    trackColors = new Float32Array(segments.length);
    trackGeometry.setAttribute('color', new BufferAttribute(trackColors, 3));
    trackGeometry.setDrawRange(0, cursor / 3);
  };

  // ─── 플레이어 ─────────────────────────────────────────────
  const playerPositions = new Float32Array(maxPlayers * 3);
  const playerColors = new Float32Array(maxPlayers * 3);
  const playerGeometry = new BufferGeometry();
  playerGeometry.setAttribute('position', new BufferAttribute(playerPositions, 3));
  playerGeometry.setAttribute('color', new BufferAttribute(playerColors, 3));
  const playerDots = new Points(playerGeometry, dotMaterial(dot, { size: 0.15, vertexColors: true }));

  // 내 말은 따로 한 번 더, 더 크고 흰색으로 겹쳐 찍는다. 색만으로는
  // 여럿이 뭉쳤을 때 내 것을 못 찾는다.
  const youPositions = new Float32Array(3);
  const youGeometry = new BufferGeometry();
  youGeometry.setAttribute('position', new BufferAttribute(youPositions, 3));
  const youDot = new Points(youGeometry, dotMaterial(dot, { color: '#f8fafc', size: 0.26 }));

  scene.add(playerDots, youDot);

  const scratch = new Color();

  // 소유 현황은 틱마다만 바뀐다. 배열의 참조로 그것을 알아채고, 바뀌었을 때만
  // 색을 다시 칠한다 — 매 프레임 수백 개 float을 쓸 이유가 없다.
  let paintedEdges: readonly PlayerId[] | null = null;
  let paintedStations: readonly PlayerId[] | null = null;

  const paintOwners = (frame: Frame) => {
    if (trackColors) {
      for (let i = 0; i < trackColors.length / 6; i++) {
        const owner = frame.edgeOwners[i] ?? 0;
        colorFor(owner, 0.42, scratch, unownedTrack);
        // 선분 하나에 정점이 둘이다. 양쪽 다 같은 색으로 칠해야 균일하게 보인다.
        for (let vertex = 0; vertex < 2; vertex++) {
          trackColors[i * 6 + vertex * 3] = scratch.r;
          trackColors[i * 6 + vertex * 3 + 1] = scratch.g;
          trackColors[i * 6 + vertex * 3 + 2] = scratch.b;
        }
      }
      trackGeometry.getAttribute('color').needsUpdate = true;
    }

    if (stationColors) {
      for (let i = 0; i < stationColors.length / 3; i++) {
        const owner = frame.stationOwners[i] ?? 0;
        colorFor(owner, 0.58, scratch, unownedStation);
        stationColors[i * 3] = scratch.r;
        stationColors[i * 3 + 1] = scratch.g;
        stationColors[i * 3 + 2] = scratch.b;
      }
      stationGeometry.getAttribute('color').needsUpdate = true;
    }
  };

  const resize = () => {
    const { clientWidth, clientHeight } = canvas;
    if (clientWidth === 0 || clientHeight === 0) {
      return;
    }
    // false: 렌더러가 캔버스의 CSS 크기를 건드리지 않게 한다. 레이아웃은 CSS가 정한다.
    renderer.setSize(clientWidth, clientHeight, false);
    camera.aspect = clientWidth / clientHeight;
    camera.updateProjectionMatrix();
  };
  resize();

  const observer = new ResizeObserver(resize);
  observer.observe(canvas);

  renderer.setAnimationLoop(() => {
    const frame = getFrame();

    if (frame.stations !== drawnStations) {
      buildMap(frame.stations, frame.edges);
      drawnStations = frame.stations;
      // 지도를 새로 만들었으니 색 버퍼도 새것이다. 다시 칠해야 한다.
      paintedEdges = null;
      paintedStations = null;
    }

    if (frame.edgeOwners !== paintedEdges || frame.stationOwners !== paintedStations) {
      paintOwners(frame);
      paintedEdges = frame.edgeOwners;
      paintedStations = frame.stationOwners;
    }

    let count = 0;
    let hasYou = false;

    for (const player of frame.players) {
      if (count >= maxPlayers) {
        break;
      }

      playerPositions[count * 3] = player.position.x;
      playerPositions[count * 3 + 1] = player.position.y;
      playerPositions[count * 3 + 2] = 0.01;

      scratch.setHSL(hueFor(player.id), 0.7, 0.62);
      playerColors[count * 3] = scratch.r;
      playerColors[count * 3 + 1] = scratch.g;
      playerColors[count * 3 + 2] = scratch.b;

      count++;

      if (player.id === frame.you) {
        youPositions[0] = player.position.x;
        youPositions[1] = player.position.y;
        youPositions[2] = 0.02;
        hasYou = true;
      }
    }

    playerGeometry.setDrawRange(0, count);
    playerGeometry.getAttribute('position').needsUpdate = true;
    playerGeometry.getAttribute('color').needsUpdate = true;

    youDot.visible = hasYou;
    youGeometry.getAttribute('position').needsUpdate = true;

    renderer.render(scene, camera);
  });

  return () => {
    renderer.setAnimationLoop(null);
    observer.disconnect();

    trackGeometry.dispose();
    stationGeometry.dispose();
    playerGeometry.dispose();
    youGeometry.dispose();
    track.material.dispose();
    stationDots.material.dispose();
    playerDots.material.dispose();
    youDot.material.dispose();
    dot.dispose();

    // GPU 자원은 GC가 회수하지 않는다. 놓치면 컨텍스트가 쌓이다 브라우저 한도에 걸린다.
    renderer.dispose();
  };
}

/**
 * 플레이어 색. 황금각(137.5°)씩 돌리면 인접한 ID끼리도 색이 확실히 갈린다.
 * 단순히 id * 30 같은 식으로 하면 12명마다 같은 색이 돈다.
 */
export function hueFor(id: PlayerId): number {
  return ((id * 137.508) % 360) / 360;
}

/** 주인이 있으면 그 사람의 색, 없으면 기본색. 밝기로 땅과 말을 구분한다. */
function colorFor(owner: PlayerId, lightness: number, out: Color, unowned: Color): void {
  if (owner === 0) {
    out.copy(unowned);
    return;
  }
  out.setHSL(hueFor(owner), 0.7, lightness);
}

/**
 * 점을 동그랗게 찍기 위한 스프라이트.
 * PointsMaterial은 기본이 정사각형이라, 지도 위의 말이 네모나게 보인다.
 */
function createDotTexture(): CanvasTexture {
  const size = 64;
  const canvas = document.createElement('canvas');
  canvas.width = size;
  canvas.height = size;

  const context = canvas.getContext('2d');
  if (context) {
    context.fillStyle = '#fff';
    context.beginPath();
    // 가장자리 2px는 비워둔다. 텍스처 경계에 딱 붙이면 확대될 때 각져 보인다.
    context.arc(size / 2, size / 2, size / 2 - 2, 0, Math.PI * 2);
    context.fill();
  }

  const texture = new CanvasTexture(canvas);
  texture.colorSpace = SRGBColorSpace;
  return texture;
}

function dotMaterial(map: CanvasTexture, options: { color?: string; size: number; vertexColors?: boolean }) {
  return new PointsMaterial({
    ...options,
    map,
    transparent: true,
    // 반투명 정렬 대신 잘라낸다. 점끼리 겹칠 때 그리는 순서에 따라
    // 뒤엣것이 앞엣것을 지우는 일을 막는다.
    alphaTest: 0.5,
  });
}
