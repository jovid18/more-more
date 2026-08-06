import {
  BufferAttribute,
  BufferGeometry,
  Color,
  LineBasicMaterial,
  LineLoop,
  PerspectiveCamera,
  Points,
  PointsMaterial,
  Scene,
  WebGLRenderer,
} from 'three';

import { ringPositions } from '@/graph/ring';

const STATION_COUNT = 30;
const RADIUS = 2.2;

/**
 * 캔버스에 야마노테선 자리표시 링을 띄운다. 정리 함수를 돌려준다.
 *
 * 지금은 툴체인이 끝까지 이어지는지 확인하는 용도다 — Vite가 three를 번들하고,
 * WebGL 컨텍스트가 잡히고, 정리가 제대로 도는지. 서버 스냅샷은 아직 안 붙었다.
 */
export function mountScene(canvas: HTMLCanvasElement): () => void {
  const renderer = new WebGLRenderer({ canvas, antialias: true });
  // 3배 이상은 눈에 띄지 않고 픽셀만 4배 이상 늘어난다.
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));

  const scene = new Scene();
  scene.background = new Color('#0b0d12');

  const camera = new PerspectiveCamera(50, 1, 0.1, 100);
  camera.position.set(0, -4.5, 4.5);
  camera.lookAt(0, 0, 0);

  // 역과 선로가 같은 geometry를 공유한다. LineLoop이 마지막 점을 첫 점에 이어주므로
  // 순환선을 따로 닫아줄 필요가 없다.
  const geometry = new BufferGeometry();
  geometry.setAttribute('position', new BufferAttribute(ringPositions(STATION_COUNT, RADIUS), 3));

  const stations = new Points(geometry, new PointsMaterial({ color: '#7dd3fc', size: 0.09 }));
  const track = new LineLoop(geometry, new LineBasicMaterial({ color: '#334155' }));
  scene.add(track, stations);

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

  renderer.setAnimationLoop((time) => {
    // 서버 틱과 무관하게 도는 로컬 애니메이션. 나중에 스냅샷 보간이 들어올 자리다.
    scene.rotation.z = time / 12_000;
    renderer.render(scene, camera);
  });

  return () => {
    renderer.setAnimationLoop(null);
    observer.disconnect();
    geometry.dispose();
    stations.material.dispose();
    track.material.dispose();
    // GPU 자원은 GC가 회수하지 않는다. 놓치면 컨텍스트가 쌓이다 브라우저 한도에 걸린다.
    renderer.dispose();
  };
}
