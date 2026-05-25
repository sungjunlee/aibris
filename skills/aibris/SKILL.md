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

**실패 처리**: 명령어가 실패하면 (tool 미설치, permission 에러 등) 사용자에게 구체적 에러를 보여주고 설치 안내 후 중단한다.
**빈 결과 처리**: `total_count`가 0이면 "정리할 항목이 없습니다" 알리고 `/clear` 안내 후 중단한다.

### Step 2: 분석 및 제시

JSON 결과를 파싱해 **크기 순으로 정렬**하여 사용자에게 보여준다.
- worktree는 `project` 필드로 그룹핑해서 보여준다
- `by_category`에 없는 카테고리는 출력에서 제외한다

```markdown
📦 전체: {total_count}개 항목 | {total_size}

[워크트리] {count}개 | {size}
  • {project}: {count}개 ({size}) — 최신 {relative_time}, 가장 오래된 것 {relative_time}

[node_modules] {count}개 | {size}
  • {path} — {size}
  ...

[build-cache] {count}개 | {size}
  • {cache_name} — {size}
  ...

[기타 캐시] {count}개 | {size}
  • {cache_name} — {size}
  ...
```

실제 프로젝트명과 수치를 위 템플릿에 채워서 보여준다.

### Step 3: 질문

**가장 큰 항목부터 우선 질문한다.** 카테고리 순서보다 전체 크기 기준으로 정렬한다.
아래 질문 형식을 참고하되, 카테고리 순서가 아닌 크기 순서로 질문한다:

- **worktree**: "{project} 워크트리가 {count}개 ({size}) 있습니다. 가장 최근 게 {relative_time} 작업한 건데, 가장 오래된 건 {relative_time} 전이에요. 지워도 될까요?"
- **node_modules**: "{path}의 node_modules가 {size}인데, 이 프로젝트 아직 사용 중인가요?"
- **build-cache**: "{cache_name} 캐시가 {size} 쌓였는데, 지우시겠어요? (단, 다음 빌드 시 다시 다운로드)"
- **pip-cache**: "{cache_name} 캐시 {size} — 지우시겠어요?"

**모두 거절 시**: "모두 유지합니다. `/clear`로 마무리할게요." 라고 안내하고 `/clear` 권장 후 중단.

### Step 4: 실행

**규칙: 절대 --dry-run 없이 실제 삭제를 실행하지 않는다.**

사용자가 특정 항목 삭제를 승인하면 먼저 `--dry-run`으로 대상 목록을 보여준다:

```bash
# 1. 미리보기
aibris clean --category worktree --tool codex --age 72h --dry-run
```

사용자가 미리보기를 확인하고 실제 삭제를 재승인하면 `--dry-run`을 제거한다:

```bash
# 2. 사용자 재승인 후 실제 실행
aibris clean --category worktree --tool codex --age 72h
```

> **중요**: `--dry-run` → 사용자 확인 → 실제 실행, 이 순서를 반드시 지킨다.

## CLI 옵션 레퍼런스

### scan

```bash
aibris scan           # 사람이 읽기 쉬운 포맷
aibris scan --json    # JSON 출력 (스킬이 파싱할 때 사용)
```

### clean

```bash
# 미리보기 (항상 먼저 실행)
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

# interactive (항목별로 y/N 확인, dry-run 내장)
aibris clean --interactive

# 실제 실행 (사용자 재승인 후)
aibris clean --category node_modules
# confirm 생략 (주의: 사용자 동의 없이 삭제)
# aibris clean --force --category node_modules
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
