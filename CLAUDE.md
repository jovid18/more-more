# CLAUDE.md

도쿄 지하철 땅따먹기. Go 동시성을 익히려고 만드는 실시간 게임 서버이고, 웹 클라이언트가 붙는다.

설계와 근거는 [README.md](./README.md)에 있다.

|               |                                                       |
| ------------- | ----------------------------------------------------- |
| `apps/server` | Go. 권위적 게임 서버. `go.mod`가 여기 있다            |
| `apps/web`    | Vite + React + three.js 클라이언트. pnpm 워크스페이스 |

## 이 프로젝트의 요점

**게임 상태에 mutex를 쓰지 않는다.** 상태는 goroutine 하나가 통째로 소유하고, 입력은 채널 fan-in, 출력은 스냅샷 fan-out으로만 오간다. race가 나면 lock을 씌우는 게 아니라 **소유권이 새는 지점을 찾는다.** 편의를 위해 `sync.Mutex`를 넣으면 이 레포를 만드는 이유가 사라진다.

HTTP 핸들러도 마찬가지다. 핸들러가 상태를 직접 읽기 시작하면 그 순간 구조가 무너진다.

## PR

**PR은 `/create-pr` 스킬로 만든다.**

`git-workflow:create-pull-request`는 회사(Ecube Labs) 템플릿이라 Jira 링크와 한글 섹션이 들어간다. **이 레포에서는 쓰지 않는다.**

**문서 수정이나 사소한 변경은 별도 PR로 쪼개지 않는다.** 작업 중에 나온 문서·설정 수정은 진행 중인 PR에 같이 넣는다. 혼자 쓰는 레포에서 PR을 나누는 이득보다 따로 머지하는 비용이 크다. 진행 중인 PR이 없을 때만 문서 단독 PR을 만든다.

**작업 브랜치를 여러 개 만들지 않는다.** 브랜치를 나누면 `.gitignore`처럼 아직 머지되지 않은 설정이 다른 브랜치에 없어서, `git add -A`가 무시돼야 할 파일을 커밋한다.

## Git 금지 사항

**머지하지 않는다.** `main` 머지는 사람이 GitHub PR 화면에서 직접 한다. PR을 여는 데까지가 Claude의 끝이다. 머지 여부를 묻지도 않고, PR URL만 알려주고 멈춘다.

- `gh pr merge` 금지
- `main`으로의 `git merge` · `git push` 금지
- `git-workflow:merge-to-develop` 금지 (회사 워크플로우. 이 레포엔 `develop`이 없다)

**force push하지 않는다.** `--force`도 `--force-with-lease`도 안 된다. 이미 푸시한 커밋을 되돌릴 때는 **`git revert`로 새 커밋을 쌓는다.** 히스토리를 다시 쓰지 않는다.

**`main`에서 커밋하지 않는다.** 작업은 항상 브랜치를 파서 한다.

강제는 **GitHub 룰셋**이 한다. public 레포라 서버에서 브랜치 보호를 걸 수 있고, 그쪽은 `--no-verify`로 뚫리지 않는다. 룰셋이 막는 것:

- `main` 직접 푸시 — PR을 거쳐야 한다
- `main` 삭제, 모든 force push
- CI(`Server` / `Web`) 통과 전 머지

로컬에는 `.githooks/pre-commit` 하나만 둔다. `main` 커밋을 막고 스테이징된 파일을 포맷한다. CI 왕복 없이 즉시 알려주는 편의 장치이고, 룰셋과 역할이 겹치지 않는다.

클론마다 한 번:

```sh
git config core.hooksPath .githooks
git config pull.ff only
```

## 커밋

- Conventional Commits (`type: description`)
- **메시지는 영어.** 본문은 `-` 불릿으로 최대 4줄
- 브랜치는 `<type>/<kebab-case>`. 항상 최신 `main`에서 분기

## 포맷

**TS·마크다운은 oxfmt, Go는 gofmt.** Prettier를 쓰지 않는다 — 전역 VS Code 설정이 Prettier(100칸)를 기본 포매터로 두면 oxfmt(120칸)와 매번 싸운다. `.vscode/settings.json`이 이 워크스페이스에서만 포매터를 덮어쓰고, `pre-commit`이 스테이징된 파일을 한 번 더 정리한다.

**표의 파이프가 원문에서 어긋나 보이는 것은 정상이다.** oxfmt가 CJK 문자 폭을 1칸으로 세기 때문이고, 렌더 결과는 정상이다. **손으로 맞추지 않는다** — 다음 저장에 다시 틀어진다.

## 검증

```sh
pnpm lint && pnpm typecheck && pnpm test && pnpm build   # 루트. 웹 전체
cd apps/server && gofmt -l . && go vet ./... && go test -race ./...
```

**`-race`를 빼지 않는다.** 이 프로젝트에서 가장 값비싼 버그가 데이터 경합이고, 그것만 도구가 사람보다 확실히 잘 잡는다.

## 문서

- 문서 본문은 한글. 코드 주석도 한글
- 커밋 메시지와 PR 본문은 영어
- 확인하지 못한 항목은 추정으로 메우지 말고 `[미확정]`으로 표시
