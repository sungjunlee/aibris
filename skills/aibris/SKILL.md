---
name: aibris
description: Scan and clean AI coding tool debris — worktrees, node_modules, build caches. Use for disk cleanup, storage management, AI tool maintenance. Triggers: aibris, disk cleanup, worktree 정리, 캐시 정리, 디스크 공간, storage full, 코딩 잔해 정리, AI debris cleanup.
---

# aibris — AI Development Debris Cleaner

AI 코딩 도구(Codex CLI, Claude Code, Cursor 등)가 남긴 작업 잔해(worktree, node_modules, build cache)를 탐지하고 정리하는 CLI.

## 사전 설치

```bash
curl -fsSL https://raw.githubusercontent.com/sungjunlee/aibris/refs/heads/main/install.sh | bash
```

기본 설치 위치는 `~/.local/bin`이며 관리자 권한이 필요 없다. PATH에
없으면 installer가 shell별 추가 명령을 안내한다. 시스템 경로에 설치해야
할 때만 명시적으로 prefix를 지정한다:

```bash
curl -fsSL https://raw.githubusercontent.com/sungjunlee/aibris/refs/heads/main/install.sh | bash -s -- --prefix /usr/local/bin
```

## 워크플로우: AI-guided 정리

사용자가 "디스크 좀 정리해줘", "오래된 워크트리 지워줘" 등의 요청을 하면 이 워크플로우를 따른다.

### Step 0: 설치 확인

```bash
command -v aibris
```

`aibris`가 없으면 사용자에게 설치 여부를 묻고, 승인 후 아래 명령을 실행한다:

```bash
curl -fsSL https://raw.githubusercontent.com/sungjunlee/aibris/refs/heads/main/install.sh | bash
```

공개 릴리스 전 main 브랜치 버전이 필요하면:

```bash
curl -fsSL https://raw.githubusercontent.com/sungjunlee/aibris/refs/heads/main/install.sh | bash -s -- main
```

### Step 1: 전체 현황 스캔

```bash
# aibris 스캔 (CLI 빌트인)
aibris scan --json

# Docker 사용 중이면 추가 스캔
docker system df 2>/dev/null
```

**실패 처리**: aibris 명령어 실패 시 사용자에게 에러를 보여주고 설치 안내 후 중단한다.
Docker는 없으면 (command not found) 무시한다.
**빈 결과 처리**: aibris `total_count`가 0이고 Docker도 없으면 "정리할 항목이 없습니다" 알리고 `/clear` 안내 후 중단한다.

### Step 2: 분석 및 제시

aibris JSON과 Docker 출력을 파싱해 **크기 순으로 정렬**하여 사용자에게 보여준다.
- worktree는 `project` 필드로 그룹핑하고 `status`, `risk`, `reason`도 함께 본다
- `active` worktree는 기본 clean 대상에서 제외되므로, 일반 정리 제안에서는 `orphaned`를 우선한다
- `by_category`에 없는 카테고리는 출력에서 제외한다
- Docker가 있으면 별도 섹션으로 추가한다

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

[ai-logs] {count}개 | {size}
  • {log_name} — {size}
  ...

[Docker] 이미지/컨테이너/볼륨 — {reclaimable_size} 해제 가능
  • docker system prune -a 로 정리
```

> ai-logs 계열은 기본적으로 clean 대상에서 제외된다.
> 삭제하려면 `--risky` 플래그가 필요하다.
> active worktree도 기본적으로 clean 대상에서 제외된다.
> 삭제하려면 `--include-active-worktrees` 플래그가 필요하다.

실제 프로젝트명과 수치를 위 템플릿에 채워서 보여준다.

### Step 3: 질문

**가장 큰 항목부터 우선 질문한다.** 카테고리 순서보다 전체 크기 기준으로 정렬한다.
아래 질문 형식을 참고하되, 카테고리 순서가 아닌 크기 순서로 질문한다:

- **worktree(orphaned)**: "{project} orphaned 워크트리가 {count}개 ({size}) 있습니다. 상위 repo metadata가 없어서 정리 후보입니다. 지워도 될까요?"
- **worktree(active)**: "{project} active 워크트리가 {count}개 ({size}) 있습니다. 기본적으로 보호됩니다. 정말 지우려면 `--include-active-worktrees`가 필요한데, 이 작업물이 필요 없는 게 맞나요?"
- **node_modules**: "{path}의 node_modules가 {size}인데, 이 프로젝트 아직 사용 중인가요?"
- **build-cache**: "{cache_name} 캐시가 {size} 쌓였는데, 지우시겠어요? (단, 다음 빌드 시 다시 다운로드)"
- **pip-cache**: "{cache_name} 캐시 {size} — 지우시겠어요?"
- **docker**: "Docker 이미지/캐시가 {size} 쌓였습니다. `docker system prune -a`로 정리할까요? (단, 실행 중인 컨테이너에 영향 없음, 이미지는 재다운로드 필요)"

**모두 거절 시**: "모두 유지합니다. `/clear`로 마무리할게요." 라고 안내하고 `/clear` 권장 후 중단.

### Step 4: 실행

**규칙: 절대 --dry-run 없이 실제 삭제를 실행하지 않는다.**

사용자가 특정 항목 삭제를 승인하면 먼저 `--dry-run`으로 대상 목록을 보여준다:

```bash
# 1. 미리보기
aibris clean --category worktree --tool codex --age 3d --dry-run
```

사용자가 미리보기를 확인하고 실제 삭제를 재승인하면 `--dry-run`을 제거한다:

```bash
# 2. 사용자 재승인 후 실제 실행
aibris clean --category worktree --tool codex --age 3d
```

> **중요**: `--dry-run` → 사용자 확인 → 실제 실행, 이 순서를 반드시 지킨다.

Docker 정리는 `docker system df`로 확인한 뒤 `docker system prune`으로 직접 실행한다:

```bash
# 미리보기
docker system df

# 실제 정리 (사용자 승인 후)
docker system prune -a

# 볼륨까지 포함하려면
docker system prune -a --volumes
```

## CLI 옵션 레퍼런스

### scan

```bash
aibris scan           # 사람이 읽기 쉬운 포맷
aibris scan --json    # JSON 출력 (스킬이 파싱할 때 사용)
aibris scan --root ~/workspace --json  # 특정 HOME 하위 경로만 스캔
```

기본 스캔 root는 `$HOME`이다. `--root`는 여러 번 지정할 수 있고, 반드시
`$HOME` 아래로 해석되어야 한다. `/`, `/tmp`, symlink escape는 거부된다.

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

# scan root 제한
aibris clean --root ~/workspace --category node_modules --dry-run

# age 필터
aibris clean --age 3d --dry-run        # 3일 이상
aibris clean --age 30d --dry-run       # 30일 이상
aibris clean --age 1mo --dry-run       # 30일 이상 (month shorthand)
aibris clean --age 1y --dry-run        # 365일 이상
aibris clean --age 1h --dry-run        # 1시간 이상 (매우 공격적, 경고 출력)

# interactive (항목별로 y/N 확인 후 실제 삭제)
aibris clean --interactive

# risky 카테고리 포함 (ai-logs, cursor, windsurf 등)
aibris clean --risky --dry-run

# active worktree까지 포함 (기본은 orphaned/legacy worktree만)
aibris clean --category worktree --include-active-worktrees --dry-run

# 실제 실행 (사용자 재승인 후)
aibris clean --category node_modules
# confirm 생략 (주의: 사용자 동의 없이 삭제)
# aibris clean --force --category node_modules
```

### categories

| Category | clean 기본? | 설명 | tool 목록 |
|----------|-----------|------|-----------|
| `worktree` | ✅ orphaned만 | AI coding tool worktrees. active는 기본 제외 | codex, claude |
| `node_modules` | ✅ 안전 | `$HOME` scan root 아래 npm project dependencies | node_modules |
| `build-cache` | ✅ 안전 | Go/Xcode/Gradle/npm/Cargo caches | build-cache |
| `other-cache` | ✅ 안전 | Python/uv pip caches | pip-cache |
| `ai-logs` | 🚫 `--risky` 필요 | AI tool session logs, file history, archived sessions | ai-logs, cursor, windsurf |

## 주의사항

- 절대 경로나 시스템 경로(`/`, `/usr`, `/etc` 등)는 삭제 방지되어 있음
- 기본 스캔 root는 `$HOME`. `--root`로 HOME 하위 경로만 좁힐 수 있음
- `$HOME` 스캔 중 `.Trash`, `Library`, `Applications`, `Pictures`, `Movies`, `Music`, `.git`, `vendor`, 중첩 `node_modules`는 가지치기함
- `Desktop`, `Downloads`는 기본 스캔에 포함됨
- 기본 `--age`는 `7d`. 명시적으로 지정하지 않으면 7일 이상된 것만 대상
- `--dry-run` 없이 실행하면 confirm 필요. `--force`로 생략 가능
- active worktree는 기본 clean 대상에서 제외. 정말 지우려면 `--include-active-worktrees` 필요
- `go-build`, `npm`, `uv` 캐시는 가능하면 공식 command(`go clean -cache`, `npm cache clean --force`, `uv cache prune`)로 정리함
- command가 없으면 기존 safe path 삭제로 fallback하지만, command가 실행 후 실패하면 조용히 fallback하지 않음
- `aibris clean`은 `os.RemoveAll()`로 디렉토리를 완전히 삭제하므로 복구 불가
- **ai-logs 계열**은 기본 clean에서 제외. 삭제하려면 `--risky` 필요

## 개발

개발 관련 사항은 `AGENTS.md` 참고.
