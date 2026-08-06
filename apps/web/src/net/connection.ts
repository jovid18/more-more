import type { ClientMessage, ServerMessage } from './protocol';
import { parseServerMessage } from './protocol';

export type ConnectionStatus = 'connecting' | 'open' | 'closed';

export interface ConnectionHandlers {
  onMessage: (message: ServerMessage) => void;
  onStatus: (status: ConnectionStatus) => void;
}

export interface Connection {
  send: (message: ClientMessage) => void;
  close: () => void;
}

const firstRetryDelayMs = 500;
const maxRetryDelayMs = 8_000;

/**
 * 게임 서버에 붙고, 끊기면 다시 붙는다.
 *
 * 재접속을 굳이 넣은 이유는 스냅샷이 델타가 아니라 전체 상태이기 때문이다.
 * 다시 붙기만 하면 다음 스냅샷 한 장으로 화면이 완전히 복구되므로, 놓친 구간을
 * 따라잡는 로직이 필요 없다. 서버를 재시작해도 탭을 새로고침할 일이 없다.
 *
 * 정리 함수를 부르면 재접속도 함께 멈춘다.
 */
export function connect(url: string, handlers: ConnectionHandlers): Connection {
  let socket: WebSocket | null = null;
  let retryTimer: ReturnType<typeof setTimeout> | null = null;
  let retryDelay = firstRetryDelayMs;
  let closedByUs = false;

  const open = () => {
    handlers.onStatus('connecting');

    const next = new WebSocket(url);
    socket = next;

    next.addEventListener('open', () => {
      retryDelay = firstRetryDelayMs;
      handlers.onStatus('open');
    });

    next.addEventListener('message', (event: MessageEvent<unknown>) => {
      if (typeof event.data !== 'string') {
        return;
      }
      const message = parseServerMessage(event.data);
      if (message) {
        handlers.onMessage(message);
      }
    });

    // error 뒤에는 반드시 close가 따라오므로 재접속은 close에서만 건다.
    // 양쪽에 걸면 소켓 하나가 끊길 때 재접속이 둘 예약된다.
    next.addEventListener('close', () => {
      if (closedByUs) {
        return;
      }

      handlers.onStatus('closed');
      retryTimer = setTimeout(open, retryDelay);
      // 서버가 죽어 있는 동안 초당 두 번씩 두드리지 않게 간격을 늘린다.
      retryDelay = Math.min(retryDelay * 2, maxRetryDelayMs);
    });
  };

  open();

  return {
    send(message) {
      if (socket?.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify(message));
      }
    },
    close() {
      closedByUs = true;
      if (retryTimer !== null) {
        clearTimeout(retryTimer);
        retryTimer = null;
      }

      const closing = socket;
      socket = null;
      if (!closing) {
        return;
      }

      if (closing.readyState === WebSocket.CONNECTING) {
        // 핸드셰이크 중에 닫으면 브라우저가 경고를 남긴다. StrictMode는 개발에서
        // effect를 두 번 돌리므로 새로고침할 때마다 콘솔이 더러워진다.
        // 열린 직후에 닫아 같은 결과를 조용히 얻는다.
        closing.addEventListener('open', () => closing.close(), { once: true });
        return;
      }

      closing.close();
    },
  };
}

/**
 * 지금 페이지와 같은 오리진의 `/ws` 주소를 만든다.
 *
 * 절대 주소를 코드에 적으면 배포에서 갈린다. 개발 중에는 Vite가 이 경로를
 * Go 서버로 프록시하므로 양쪽에서 같은 문자열이 그대로 통한다.
 */
export function gameSocketURL(name: string): string {
  const url = new URL('/ws', window.location.href);
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
  url.searchParams.set('name', name);
  return url.toString();
}
