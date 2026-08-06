# apps/server

권위적(authoritative) 게임 서버. 설계는 루트 [README.md](../../README.md)에 있다.

## 실행

```sh
cd apps/server

go run ./cmd/server            # :8080
go run ./cmd/server -addr :9000

go test -race ./...            # 동시성이 요점이므로 -race는 항상 켠다
go vet ./...
gofmt -l .                     # 출력이 있으면 포맷이 어긋난 파일이다
```

붙어보려면 브라우저 콘솔에서:

```js
const ws = new WebSocket('ws://localhost:8080/ws?name=hana');
ws.onmessage = (e) => console.log(JSON.parse(e.data));
ws.send(JSON.stringify({ type: 'reverse' })); // 방향 전환
```

## 배치

|                   |                                                             |
| ----------------- | ----------------------------------------------------------- |
| `cmd/server`      | 플래그·시그널 처리. 로직은 두지 않는다                      |
| `internal/graph`  | 역·구간 그래프. 순수 데이터라 여럿이 동시에 읽어도 안전하다 |
| `internal/game`   | 상태를 소유한 goroutine 하나. 틱 진행과 방송                |
| `internal/hub`    | WebSocket 연결. 연결당 읽기·쓰기 goroutine 한 쌍            |
| `internal/server` | HTTP 표면과 프로세스 수명                                   |

`go.mod`는 레포 루트가 아니라 여기 있다. `apps/web`이 Node 워크스페이스라 루트를
한쪽 언어에 내주지 않으려는 것이고, 대신 Go 명령은 전부 이 디렉터리에서 돌린다.

## 상태가 흐르는 길

```
연결 N개 ─ 읽기 goroutine ─┐
                           ├─→ commands 채널 (fan-in) ─→ [ 상태 소유 goroutine ] ─┐
                    ticker ─┘                                                      │
                                                                                   │
연결 N개 ← 쓰기 goroutine ←─ 구독 채널 N개 (fan-out) ←──── 매 틱 스냅샷 ──────────┘
```

**들어오는 길은 하나다.** 접속·이탈·방향 전환·틱이 전부 같은 `select`로 모이므로
판정 순서가 한 곳에서 정해진다. 같은 역에 동시 도착해도 경합이 생길 수 없다.

**나가는 길은 절대 막히지 않는다.** 구독 채널은 버퍼 1이고, 차 있으면 밀린
스냅샷을 버리고 최신 것을 넣는다. 스냅샷이 델타가 아니라 전체 상태라서
중간을 건너뛰어도 잃는 정보가 없다. 느린 연결 하나가 게임 전체를 세우는 일이
없어야 하고, 이게 그 보장이다.

읽기도 예외가 아니다. `Loop.Snapshot`은 상태를 직접 보지 않고 명령 채널을 타고
들어간다 — 지름길을 내는 순간 잠금이 필요해진다.

## 프로토콜

접속하면 지도가 한 번 오고, 그 뒤로는 매 틱 스냅샷이 온다.

```jsonc
// 서버 → 클라이언트, 접속 직후 한 번
{ "type": "welcome", "you": 1, "tickMs": 500, "stations": [...], "edges": [...] }

// 서버 → 클라이언트, 매 틱
{ "type": "snapshot", "tick": 42,
  "players": [{ "id": 1, "name": "hana",
                "move": { "from": 3, "to": 4, "progress": 1, "needTicks": 3 } }] }

// 클라이언트 → 서버
{ "type": "reverse" }
```

역은 인덱스로만 오간다. 지도는 접속할 때 한 번 받았으므로, 매 틱 오가는 양이
플레이어 수에만 비례한다.

**`move`에 좌표가 없는 것이 핵심이다.** 서버는 "3틱 중 1틱째"까지만 알고, 그
사이를 부드럽게 잇는 것은 클라이언트가 `tick`을 기준으로 보간해서 한다. 서버가
느려도 화면은 매끄럽고, 판정은 정수 비교라 애매한 경계가 생기지 않는다.

## 규칙

**게임 상태에 mutex를 쓰지 않는다.** race가 나면 lock을 씌우는 게 아니라
소유권이 새는 지점을 찾는다 — 이 구조를 익히는 것이 프로젝트의 목적이다.

## 아직 없는 것

점령·경합 판정(로드맵 2)이 들어갈 자리는 `game.state.advance`의 도착 지점이다.
지금은 도착하면 곧바로 다음 구간에 오르기만 한다.
