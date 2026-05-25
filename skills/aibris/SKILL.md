---
name: aibris
description: Scan and clean AI coding tool debris — worktrees, node_modules, build caches. Use for disk cleanup, storage management, AI tool maintenance. Triggers: aibris, disk cleanup, worktree 정리, 캐시 정리, 디스크 공간, storage full, 코딩 잔해 정리, AI debris cleanup.
---

# aibris — AI Worktree Debris Cleaner

AI 코딩 도구(Codex CLI, Claude Code, Cursor 등)가 남긴 작업 잔해(worktree, node_modules, build cache)를 탐지하고 정리하는 CLI.

## 사전 설치

```bash
go build -o aibris .
# 또는
brew install sungjunlee/tap/aibris
```

## 워크플로우: AI-guided 정리

사용자가 "디스크 좀 정리해줘", "오래된 워크트리 지워줘" 등의 요청을 하면 이 워크플로우를 따른다.

### Step 1: 전체 현황 스캔

```bash
aibris scan --json
```

### Step 2: 분석 및 제시

JSON 결과를 파싱해 다음과 같이 사용자에게 보여준다:

```markdown
📦 전체: 23개 항목 | 6.0 GB

[워크트리] 18개 | 5.9 GB
  • beopjalal: 3개 (2.2 GB) — 최신 today, 가장 오래된 것 2d ago
  • dev-relay: 4개 (13 MB) — 모두 1d ago
  • tamgu_note: 1개 (3.2 GB) — 2d ago
  ...

[node_modules] 2개 | 15 MB
  • proj-a/node_modules — 10 MB
  • proj-b/node_modules — 5 MB

[build-cache] 1개 | 500 MB
  • go-build — 500 MB

[기타 캐시] 2개 | 120 MB
  • pip — 100 MB
  • uv — 20 MB
```

### Step 3: 질문

사용자 상황에 맞는 질문을 던진다:

- **worktree**: "beopjalal 워크트리가 3개나 됩니다 (2.2 GB). 가장 최근 게 오늘 작업한 건데, 나머지 2개는 2일 전 거예요. 지워도 될까요?"
- **node_modules**: "proj-a의 node_modules가 10MB인데, 이 프로젝트 아직 사용 중인가요?"
- **build-cache**: "go-build 캐시가 500MB 쌓여있는데, go clean -cache로 정리해도 될까요? (단, 다음 빌드 시 다시 다운로드)"
- **pip-cache**: "pip 캐시 100MB, uv 캐시 20MB — 지우시겠어요?"

### Step 4: 실행

답변을 바탕으로 적절한 CLI 명령어를 조합한다:

```bash
# worktree 중 codex의 3일 이상 된 것만 미리보기
aibris clean --category worktree --tool codex --age 72h --dry-run

# node_modules 전체 미리보기
aibris clean --category node_modules --dry-run

# 특정 category만 캐시 정리
aibris clean --category build-cache,other-cache --dry-run

# 확인 후 실제 실행 (--dry-run 제거)
aibris clean --category worktree --tool codex --age 72h
```

> **중요**: 실제 삭제 전에는 항상 `--dry-run`으로 미리보기를 먼저 실행한다.

## CLI 옵션 레퍼런스

### scan

```bash
aibris scan           # 사람이 읽기 쉬운 포맷
aibris scan --json    # JSON 출력 (스킬이 파싱할 때 사용)
```

### clean

```bash
# 미리보기
aibris clean --dry-run

# category 필터 (여러 개: 쉼표)
aibris clean --category worktree --dry-run
aibris clean --category node_modules,build-cache --dry-run

# tool 필터
aibris clean --tool codex --dry-run

# category + tool AND 조합
aibris clean --category worktree --tool codex --dry-run

# age 필터
aibris clean --age 72h --dry-run       # 3일 이상
aibris clean --age 720h --dry-run      # 30일 이상
aibris clean --age 0h --dry-run        # 전체

# interactive (항목별 확인)
aibris clean --interactive

# 실제 실행 (confirm 필요)
aibris clean --category node_modules
# 또는 confirm 생략
aibris clean --force --category node_modules
```

### categories

| Category | 설명 | tool 목록 |
|----------|------|-----------|
| `worktree` | AI coding tool worktrees | codex, claude, cursor, windsurf |
| `node_modules` | npm project dependencies | node_modules |
| `build-cache` | Go/Xcode build caches | build-cache |
| `other-cache` | Python/uv pip caches | pip-cache |

## 주의사항

- 절대 경로나 시스템 경로(`/`, `/usr`, `/etc` 등)는 삭제 방지되어 있음
- 기본 `--age`는 `168h` (7일). 명시적으로 지정하지 않으면 7일 이상된 것만 대상
- `--dry-run` 없이 실행하면 confirm 필요. `--force`로 생략 가능
- `aibris clean`은 `os.RemoveAll()`로 디렉토리를 완전히 삭제하므로 복구 불가

## 개발

개발 관련 사항은 `AGENTS.md` 참고.
