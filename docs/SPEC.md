# aibris — Engineering Spec

## Goal

AI 코딩 도구(Codex CLI, Claude Code)가 git worktree 격리 작업 후 남긴 디렉토리
잔해를 탐지(`scan`)하고 조건에 따라 정리(`prune`)하는 단일 바이너리 CLI 도구.

`npx skills`로 설치 가능한 범용 스킬을 함께 제공해 Claude Code, Codex CLI 등
AI 코딩 도구 안에서 트리거로 바로 호출할 수 있게 한다.

## Non-goal

- Cursor, Windsurf adapter — worktree 경로가 아직 알려지지 않음 (TBD)
- GUI, Web UI, 데몬, 스케줄러, 원격 스캔, worktree 생성/관리
- Git 명령어 직접 수행 (`git worktree list` 등)
- 새로운 AI 도구를 위한 adapter 자동 생성
- Homebrew formula / goreleaser 설정 (이미 작성됨, 검증만 필요)

---

## Functional Requirements

### FR1 — `aibris scan`

- 모든 등록된 adapter의 `Scan()`을 순회 실행
- 도구별로 그룹화해 출력: `codex:`, `claude:` 등
- 각 worktree는 `ID`, `Project`, `Size`, `Age` 표시
- 결과는 크기 내림차순 정렬
- 프로젝트명 추론 불가 시 `?` 표시
- 발견된 worktree 없으면 `No AI tool worktrees found.` 출력, 종료 코드 0
- adapter 하나가 실패해도 나머지는 계속 진행; 실패 정보는 stderr에 출력
- Age가 24h 미만이면 `today`, 그 이상이면 `Xd` 포맷

### FR2 — `aibris prune`

#### 플래그

| 플래그 | 숏 | 기본값 | 동작 |
|--------|-----|--------|------|
| `--age` | `-a` | `168h` | `time.ParseDuration`으로 파싱해 필터 기준으로 사용 |
| `--tool` | `-t` | `""` | 쉼표 구분 도구명. `--all`과 동시 사용 불가 |
| `--all` | | `false` | 등록된 모든 도구 대상 |
| `--dry-run` | | `false` | 삭제 없이 대상 목록 + 예상 해제 용량 출력 |
| `--force` | `-f` | `false` | confirm 프롬프트 생략하고 즉시 삭제 |
| `--interactive` | `-i` | `false` | 항목별 y/N 확인 후 삭제 |

#### 동작 순서

1. `scanner.Scan()`으로 전체 스캔
2. `cleaner.Filter()`로 `--age` 기준 필터링
3. 대상이 0개면 `No worktrees to prune.` 출력 후 종료
4. `--dry-run`이면 `DryRun()` 호출 (삭제 없음)
5. `--interactive`면 각 항목마다 `Remove <id> (<tool>) [<size>]? [y/N]:` 확인
6. `--force`가 아니면 `WARNING: This will remove N worktrees (<size>)` + `Proceed? [y/N]:` 출력
7. `cleaner.Execute()`로 `os.RemoveAll` 호출
8. 최종 해제 용량 출력: `Freed: <size>`

#### 유효성 검사

- `--age` 파싱 실패 시 `invalid age: <value>` stderr 출력 후 종료 코드 1
- `--tool` + `--all` 동시 사용은 허용하지 않음 (`--tool`이 우선)

### FR3 — `npx skills` 스킬

`skills/aibris/SKILL.md`로 제공. YAML frontmatter에 `name: aibris`, `description` 포함.
트리거 키워드: `scan-worktrees`, `prune-worktrees`, `aibris`, `worktree cleanup`,
`AI debris`, `코딩 잔해 정리`, `워크트리 정리`.

---

## Non-Functional Requirements

### NFR1 — Context 취소 존중

- 모든 `Scan(ctx)` 구현은 `ctx.Done()` 채널을 확인해야 함
- 취소 감지 시 가능한 빨리(진행 중인 I/O 완료 후) `ctx.Err()` 반환
- adapter가 context 취소를 존중하지 않으면 scanner 레벨에서 `select`로 대응

### NFR2 — 에러 전파 정책

| 레이어 | 정책 |
|--------|------|
| adapter `Scan()` | os.IsNotExist → nil, nil (경로 없으면 정상). 그 외 에러 → 그대로 반환 |
| scanner `Scan()` | adapter 에러 발생 시 stderr에 `scan:<tool>:<err>` 출력, 나머지 adapter 계속 진행 |
| cmd | scanner 에러는 stderr 출력 후 os.Exit(1). adapter 개별 에러는 이미 scanner에서 처리됨 |

### NFR3 — 코드 품질

- 함수 20줄 이하 (AGENTS.md 규칙)
- `go vet ./...` 통과
- `strings` 패키지 사용 (현재 `prune.go`의 수동 `split`/`trimSpace`를 `strings.Split`/`strings.TrimSpace`로 교체)
- 테스트 커버리지: adapter, cleaner, scanner 패키지에 `*_test.go` 작성
- 테스트는 `go test ./...`로 실행

### NFR4 — 이식성

- Go 1.26.3, 의존성: cobra v1.10.2
- `os.UserHomeDir()` 기반 경로만 사용 (절대 경로 하드코딩 금지)
- Windows에서도 빌드/동작 가능 (`filepath.Join`, `os.RemoveAll` 사용)

---

## Implementation Tasks

### T1 — Claude adapter에 프로젝트명 탐지 추가

**파일:** `internal/adapter/claude.go`

**현재 상태:** `Project` 필드를 채우지 않음. 항상 빈 문자열.

**해야 할 일:**
- `detectProjectName()`을 호출해 `w.Project` 설정
- `detectProjectName()`은 worktree 경로가 아닌 **부모 프로젝트 디렉토리**에서 실행해야 함
  - 예: `~/myproject/.claude/worktrees/feat-x` → `detectProjectName("~/myproject")`
  - worktree 경로에서 `filepath.Dir(filepath.Dir(filepath.Dir(match)))`로 프로젝트 루트 추출

**수락 기준:**
- `aibris scan` 출력에서 Claude worktree의 Project 열이 `?`가 아닌 실제 값으로 표시됨

### T2 — Context 취소 존중

**파일:** `internal/adapter/codex.go`, `internal/adapter/claude.go`

**현재 상태:** `ctx context.Context`를 받지만 사용하지 않음.

**해야 할 일:**
- `Scan()` 진입 직후 `select { case <-ctx.Done(): return nil, ctx.Err(); default: }` 추가
- 디렉토리 순회 루프 내에서 매 반복마다 context 확인
- `scanner.Scan()`에서도 provider 루프 시작 시 context 확인

**수락 기준:**
- 취소된 context를 전달하면 `context.Canceled` 에러가 반환됨
- 정상 context에서는 기존 동작 그대로

### T3 — Scanner 에러 처리 개선

**파일:** `internal/scanner/scanner.go`

**현재 상태:**
```go
worktrees, err := p.Scan(ctx)
if err != nil {
    continue  // 에러 무시
}
```

**해야 할 일:**
- `continue` 대신 stderr에 `fmt.Fprintf(os.Stderr, "scan:%s:%v\n", p.Name(), err)` 출력
- ctx 취소 에러는 즉시 상위로 반환 (`return nil, err`)

**수락 기준:**
- adapter 실패 시 stderr에 `scan:codex:<error message>` 출력됨
- ctx 취소 시 `context.Canceled`가 cmd까지 전파됨

### T4 — `strings` 패키지로 교체

**파일:** `cmd/prune.go`

**현재 상태:** `split()`, `trimSpace()`, `splitAndTrim()` 직접 구현 (36줄)

**해야 할 일:**
- `strings.Split(s, ",")` + `strings.TrimSpace()` 로 교체
- 불필요한 헬퍼 함수 3개 제거
- `import "strings"` 추가

**수락 기준:**
- `prune.go`에서 직접 구현한 문자열 함수가 사라짐
- `go vet ./...` 통과

### T5 — `LastCommit` 필드 제거

**파일:** `internal/types/types.go`

**현재 상태:** `LastCommit *time.Time` 필드 정의됨, 어디서도 사용 안 함

**해야 할 일:**
- `WorktreeInfo`에서 `LastCommit` 필드 제거

**수락 기준:**
- 컴파일 에러 없음

### T6 — 테스트 작성

**대상 패키지:**
- `internal/types/` — 타입 정의는 테스트 불필요
- `internal/adapter/` — `detectProjectName`, `isHiddenDir`, `estimateDirSize` 단위 테스트 + adapter 통합 테스트 (tmp dir에 mock worktree 생성)
- `internal/scanner/` — mock adapter로 `Scan()` 동작 검증
- `internal/cleaner/` — `Filter`, `FormatSize`, `containsTool` 단위 테스트
- `cmd/` — (제외; cobra command는 통합 테스트 범위가 넓음)

**최소 커버리지:** `internal/adapter/util.go` 3개 함수 + `cleaner.go` 4개 함수

**수락 기준:**
- `go test ./...` 통과
- `internal/adapter/util_test.go` — `TestDetectProjectName`, `TestIsHiddenDir`, `TestEstimateDirSize`
- `internal/cleaner/cleaner_test.go` — `TestFilter`, `TestFormatSize`, `TestContainsTool`

### T7 — git init

**해야 할 일:**
```bash
git init && git add -A && git commit -m "Initial commit: aibris v0.1.0"
```

---

## Architecture

```
main.go          → cmd.Execute()
cmd/
  root.go        → root command "aibris", registers scan + prune
  scan.go        → scan subcommand, 출력 포맷
  prune.go       → prune subcommand, 플래그 파싱, confirm, execute
internal/
  adapter/
    adapter.go   → WorktreeProvider interface
    codex.go     → CodexAdapter: ~/.codex/worktrees/<hash>/
    claude.go    → ClaudeAdapter: ~/*/.claude/worktrees/<name>/
    util.go      → estimateDirSize, detectProjectName, isHiddenDir
  scanner/
    scanner.go   → Scan(): providers 순회, 결과 취합, 정렬
  cleaner/
    cleaner.go   → Filter, DryRun, Execute, FormatSize, containsTool
  types/
    types.go     → Tool, WorktreeInfo, ScanResult, PruneOptions
skills/
  aibris/
    SKILL.md     → npx skills 스킬 정의
docs/
  SPEC.md        → 사용자 가이드
```

**확장 방법:** 새 AI 도구 지원은 `adapter/<name>.go` + `scanner.go`의 `providers`에 등록. 인터페이스 변경 불필요.

---

## Acceptance Criteria (전체)

1. `go build ./...` 성공
2. `go vet ./...` 성공
3. `go test ./...` 성공
4. `aibris scan` — Claude worktree에 프로젝트명 표시됨
5. `aibris prune --age 1h --dry-run` — 정상 출력
6. `aibris prune --age invalid` — stderr에 에러 메시지, 종료 코드 1
7. `skills/aibris/SKILL.md` — YAML frontmatter 포함, `ai-worktree` 레퍼런스 없음
8. `.gitignore` + `Makefile` — `ai-worktree` 레퍼런스 없음
9. 코드 내 `ai-worktree` 문자열 없음 (`grep -r "ai-worktree" --include="*.go"` 결과 0건)

---

## Dependencies

- `github.com/spf13/cobra` v1.10.2 — CLI framework
- Go 1.26.3 stdlib — `os`, `path/filepath`, `context`, `strings`, `time`, `fmt`, `sort`, `testing`

## Out of Scope (명시적 제외)

- Cursor, Windsurf adapter
- `context` 패키지 도입 (이미 사용 중)
- `goreleaser` / Homebrew formula 수정 (기존 파일 유지)
- `README.md` 수정
- `main.go` 수정
