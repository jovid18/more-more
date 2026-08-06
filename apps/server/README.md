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

## 배치

|                   |                                                        |
| ----------------- | ------------------------------------------------------ |
| `cmd/server`      | 플래그·시그널 처리. 로직은 두지 않는다                 |
| `internal/server` | HTTP 표면. 상태를 직접 만지지 않고 채널로만 이야기한다 |

`go.mod`는 레포 루트가 아니라 여기 있다. `apps/web`이 Node 워크스페이스라 루트를
한쪽 언어에 내주지 않으려는 것이고, 대신 Go 명령은 전부 이 디렉터리에서 돌린다.

## 규칙

**게임 상태에 mutex를 쓰지 않는다.** 상태는 goroutine 하나가 소유하고, 입력은
채널 fan-in, 출력은 스냅샷 fan-out으로만 오간다. race가 나면 lock을 씌우는 게
아니라 소유권이 새는 지점을 찾는다 — 이 구조를 익히는 것이 프로젝트의 목적이다.
