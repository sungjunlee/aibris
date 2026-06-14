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

각 `adapter`는 `DebrisProvider` 인터페이스를 구현하며, 새 AI 도구가 나오면 adapter만 추가하면 된다.

## 개발 규칙

**1. Adapter 추가시 꼭 지킬 것**
- `internal/adapter/<name>.go` 에 `DebrisProvider` 구현
- `Name()`은 kebab-case 단일 소문자 (e.g. `codex`, `claude`)
- `Scan()`은 context 취소를 존중해야 함
- 발견된 모든 경로의 크기를 `estimateDirSize()`로 계산 (WalkDir 기반)
- 프로젝트명은 `detectProjectName()`으로 추론 (숨김 디렉토리 제외)
- `internal/scanner/scanner.go` 의 `providers` 목록에 등록

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
| worktree (codex) | worktree | ✅ | `~/.codex/worktrees/<hash>/` |
| worktree (claude) | worktree | ✅ | `~/*/.claude/worktrees/<name>/` |
| worktree (generic) | worktree | ✅ | `*/worktree*/*` — relay, project-local, and unknown tools auto-discovered |
| cursor | ai-logs | 🚫 `--risky` | `~/.cursor/projects/<name>/` |
| windsurf | ai-logs | 🚫 `--risky` | `~/.codeium/windsurf/` |
| node_modules | node_modules | ✅ | `~/projects/**/node_modules/` |
| build-cache | build-cache | ✅ | `~/.cache/go-build/`, `~/.gradle/caches/`, `~/.npm/_cacache/`, `~/.cargo/registry/`, `~/Library/Caches/Xcode/` |
| pip-cache | other-cache | ✅ | `~/.cache/pip/`, `~/.cache/uv/` |
| ai-logs | ai-logs | 🚫 `--risky` | `~/.codex/logs_2.sqlite`, `~/.codex/archived_sessions/`, `~/.claude/command-audit.log`, `~/.claude/file-history/` |

### Worktree health

`WorktreeAdapter`는 각 worktree의 `.git` 파일을 읽어 상위 repo 생존 여부를 확인합니다:

| Status | 의미 |
|--------|------|
| `active` | `.git` 존재, 상위 repo 살아있음 |
| `orphaned` | `.git` 존재, 상위 repo 사라짐 (정리 대상) |
| `plain-dir` | `.git` 없음 (일반 디렉토리, 무시) |

## 빌드

```bash
go build -o aibris .
./aibris scan
./aibris clean --dry-run
```
