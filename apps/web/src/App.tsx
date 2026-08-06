import { useEffect, useRef } from 'react';

import { mountScene } from '@/scene/mountScene';

export function App() {
  const canvasRef = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    // StrictMode는 개발 중 effect를 두 번 돌린다. mountScene이 돌려주는 정리 함수를
    // 그대로 반환해 WebGL 컨텍스트가 둘 남지 않게 한다.
    return canvas ? mountScene(canvas) : undefined;
  }, []);

  return <canvas ref={canvasRef} style={{ display: 'block', inlineSize: '100%', blockSize: '100%' }} />;
}
