# AGENTS.md

`aibris` (AI + debris). AI 코딩 도구들의 작업 잔해(worktree, node_modules, build cache)를
탐지+정리하는 Go CLI.

## AI-guided 정리 워크플로우 (사용자 요청 시)

사용자가 "디스크 정리", "오래된 워크트리 삭제" 등을 요청하면
`skills/aibris/SKILL.md`의 워크플로우에 따라 진행한다:

```
1. aibris scan --json  → 전체 현황 파악
2. 항목별 분석 제시     (프로젝트/크기/경과시간)
3. 질문으로 의도 구체화  (이거 지워도 되나요?)
4. aibris clean --flag  → 적절한 옵션으로 실행
```

CLI 자체는 dumb executor. Q&A와 판단은 AI 스킬이负责.

## 동작 원리

```
사용자 입력 → cobra 커맨드 (cmd/) → scanner.Scan() → adapter 각각 스캔
                                   → cleaner.Filter() + Execute()
```

각 `adapter`는 `DebrisProvider` 인터페이스를 구현한다. Worktree는 특정 도구별 adapter를
계속 늘리기보다 bounded `$HOME` convention fallback과 finite exact container
registry를 함께 사용하고 `.git` metadata로 검증한다.

## 개발 규칙

**1. Adapter 추가시 꼭 지킬 것**
- `internal/adapter/<name>.go` 에 `DebrisProvider` 구현
- `Name()`은 kebab-case 단일 소문자 (e.g. `codex`, `claude`)
- `Scan()`은 context 취소를 존중해야 함
- 발견된 모든 경로의 크기를 `estimateDirSize()`로 계산 (WalkDir 기반)
- 중첩 캐시 트리(build cache, pip/uv cache)와 agent-state project store는 트리 내부 최신 mtime을 `ModTime`으로 보고하며, 컨테이너 mtime만 활동 신호로 사용하지 않는다. 컨테이너 자체 mtime이 곧 활동인 adapter(`node_modules` 등)에는 적용되지 않는다
- `ModTime`을 트리 내부 활동에서 유도하는 adapter는 `PathModTime`에 경로 자체의 stat mtime을 반드시 채운다 (비우면 cleanup preflight가 `ModTime`을 컨테이너 mtime으로 덮어써 활동 신호가 사라진다)
- worktree 컨테이너처럼 프로젝트가 하위 디렉토리인 adapter는 `detectProjectName()`으로 추론 (숨김 디렉토리 제외)
- recorded cwd 자체가 프로젝트를 가리키는 store adapter는 `projectNameFromRecordedCWD()`로 마지막 경로 조각을 사용 (파일시스템 조회 금지)
- `internal/adapter/providers.go` 의 `providers` 목록에 등록
- `Category()`가 `agent-state`인 adapter는 `AgentStateRevalidator`도 구현 (`agent-state` 분류는 recorded cwd 부재 증명 기반이고, `--age`는 적용되지 않으며, `--agent-state-grace` 최소 idle age는 기본 선택만 제한한다. 등록된 revalidator가 없으면 삭제 거부)

**1-1. Worktree discovery 변경시 꼭 지킬 것**
- known deep container는 finite exact registry로만 추가한다. 현재 registry는 `~/.codex/worktrees`(codex 컨테이너는 `$CODEX_HOME`, `$AIBRIS_CODEX_HOMES` home 기준), `~/.relay/worktrees`, `~/.gstack/worktrees`, `~/.config/superpowers/worktrees`
- generic fallback은 `$HOME` 아래 `worktrees`, `worktree`, `worktree-*`, `worktrees-*` 디렉토리를 찾고 `maxWorktreeContainerDepth=4`를 유지한다
- hidden owner 디렉토리(`.codex`, `.somename` 등)는 worktree source일 수 있으므로 일반적으로 숨김이라는 이유만으로 prune하지 않는다
- 전체 `$HOME`이나 hidden owner를 무제한 재귀 탐색하지 않는다. hidden owner는 immediate convention child까지만 확인한다
- 후보는 direct `<entry>/.git` 또는 nested `<entry>/<project>/.git` 파일이 있어야 한다. registered container owner는 `<owner>/<leaf>/<checkout>/.git` 두 단계까지 허용한다
- member 탐색은 convention fallback에서 direct 또는 one-level nested까지만 허용한다. registered container만 two-level member를 본다. 전체 `$HOME` 재귀는 하지 않는다
- outer `<entry>` 하나가 물리 mutation owner 하나다. valid/invalid marker가 섞이면 valid sibling을 내보내지 않고 owner 하나를 `plain-dir`로 보고한다. 빈 leftover member(엔트리 없음)는 invalid marker가 아니다. registered sidecar 이름은 finite exact registry이며 현재 `.orca-worktree-trash`만 해당한다. sidecar는 내용이 있어도 member 분류에서 건너뛴다
- readable missing/empty/malformed/directory marker는 explicit `Reason`이 있는 review-only `plain-dir`; I/O 실패는 provider error/partial scan이다
- `.git` 파일의 `gitdir:`를 읽어 `active`/`orphaned`를 판정한다. referenced gitdir가 없으면 `orphaned`
- `Source`는 path-derived owner(`.codex`, `.somename`, `project-local`) 또는 registered `superpowers`로 채운다
- `plain-dir`, empty, unknown worktree status는 age/`--risky`/`--include-active-worktrees`와 무관하게 절대 정리 후보가 아니다
- explicit `--root`는 hard boundary다. `appendUncoveredCodexHomes`는 기본 `$HOME` 스캔에만 적용한다. 명시적 root가 Codex home을 포함하지 않으면 한 줄 diagnostic만 내고 범위를 넓히지 않는다. `--root`가 valid worktree outer owner이면 그 unit 하나를 발견한다

**2. Prune 안전장치**
- 기본 `--age`는 `7d`
- `--dry-run` 없이 실행 시 confirm 요청
- `--force`로만 confirm 생략 가능
- `--interactive`는 항목별 y/N 확인
- 절대 경로나 시스템 경로 삭제 금지

**3. 코드 규칙**
- 불필요한 추상화 금지. 인터페이스는 진짜 확장 지점에만
- 에러 처리는 가능한 시나리오에만. "일어날 수 없는" 에러는 무시
- 인접 코드 "개선" 금지. 시키지 않은 리팩터링 금지
- 기존 스타일 유지. tab indentation, Go 표준 포맷
- 새 패키지 추가시 `go mod tidy` 필수

**4. 작업 순서**
1. 무슨 일인지 명확히 파악
2. 플랜을 1-2문장으로 말하고 확인
3. 구현
4. `go build ./...` 로 컴파일 확인
5. `go vet ./...` 로 정적 분석

## 구조

```
cmd/         → cobra commands (root, scan, clean)
internal/
  adapter/   → DebrisProvider 인터페이스 + codex, claude 등 구현
  scanner/   → Scan(): 전체 adapter 순회하며 수집
  cleaner/   → Filter(): 조건에 따라 필터, Execute() 삭제
  types/     → DebrisInfo, ScanResult, PruneOptions
skills/
  aibris/    → AI-assisted 정리 워크플로우 (SKILL.md)
```

## 경로 규칙

| Tool | Category | clean 기본 | 기본 경로 |
|------|----------|-----------|---------|
| worktree (registry + convention) | worktree | orphaned만 ✅ | finite exact registry + depth-4 `{worktrees,worktree,worktree-*,worktrees-*}/<entry>/` fallback; direct/one-level `.git`, plus two-level `<owner>/<leaf>/<checkout>/.git` inside registered containers only |
| claude | agent-state | orphaned만 ✅ (분류는 증명 기반; `--age` 미적용; `--agent-state-grace`가 기본 선택을 지연; live/undetermined 보호) | `~/.claude/projects/<name>/` |
| cursor | agent-state | orphaned만 ✅ (분류는 증명 기반; `--age` 미적용; `--agent-state-grace`가 기본 선택을 지연; live/undetermined 보호) | `~/.cursor/projects/<name>/` |
| windsurf | ai-logs | 🚫 `--risky` | `~/.codeium/windsurf/` |
| node_modules | node_modules | ✅ | `$HOME/**/node_modules/` with noisy directories pruned |
| build-cache | build-cache | ✅ | process `$GOCACHE`, else `go env -w` file, else `UserCacheDir/go-build` (Linux `~/.cache/go-build`, Darwin `~/Library/Caches/go-build`, Windows `%LocalAppData%\go-build`); configured GOCACHE outside roots is skipped; `~/.gradle/caches/`, `~/.npm/_cacache/`, `~/.cargo/registry/`, `~/Library/Caches/Xcode/`, `~/Library/Caches/Homebrew/` (`brew cleanup --prune=all`), `~/Library/Developer/Xcode/DerivedData/`, `~/Library/Caches/CocoaPods/`, `~/.dartServer/` (never `~/.pub-cache`) |
| pip-cache | other-cache | ✅ | `~/.cache/pip/`, `~/.cache/uv/` |
| ai-logs | ai-logs | 🚫 `--risky` | `$CODEX_HOME/logs_2.sqlite`, `$CODEX_HOME/archived_sessions/` (`$CODEX_HOME` 기본값 `~/.codex`; `$AIBRIS_CODEX_HOMES` 추가 home 지원), `~/.claude/command-audit.log`, `~/.claude/file-history/` |

### Worktree health

`WorktreeAdapter`는 각 worktree의 `.git` 파일을 읽어 상위 repo 생존 여부를 확인합니다.
`source`는 `.codex`, `.claude`, `.somename`, `project-local`처럼 경로에서 추론하며
registered superpowers container는 `superpowers`를 사용합니다:

| Status | 의미 |
|--------|------|
| `active` | `.git` 존재, 상위 repo 살아있음 (최근 사용·머지 여부가 아님) |
| `orphaned` | `.git` 존재, 상위 repo 사라짐 (정리 대상) |
| `plain-dir` | valid metadata 없음 또는 한 owner 안의 invalid/mixed marker (review-only, 정리 금지) |

Guided cleanup treats unique-vs-`refs/remotes/origin/HEAD` (or unknown uniqueness)
as `reviewable`, never auto-recommended. It does not call GitHub. Scan `active`
stays gitdir liveness. All-merged units are not promoted past keep=3 / min-idle /
min-size / recent locks.

## 빌드

```bash
go build -o aibris .
./aibris scan
./aibris clean --dry-run
```
