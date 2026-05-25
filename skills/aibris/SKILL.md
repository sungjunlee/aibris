---
name: aibris
description: Scan and prune git worktree debris left by AI coding tools (Codex CLI, Claude Code, Cursor). Use when you need to find or clean up stale worktrees, check disk usage from AI tools, or do maintenance cleanup. Triggers: scan-worktrees, prune-worktrees, aibris, worktree cleanup, AI debris, 코딩 잔해 정리, 워크트리 정리.
---

# aibris — AI Worktree Cleaner

AI 코딩 도구가 작업 격리를 위해 생성한 worktree 디렉토리를 탐지하고
오래된 것만 골라 정리한다.

## 사전 설치

```bash
brew install sungjunlee/tap/aibris
# 또는
go install github.com/sungjunlee/aibris@latest
```

## 스캔

워크트리 현황 확인이 필요하면 실행:

```bash
aibris scan
```

## 정리

**반드시 먼저 미리보기로 확인한 후 실제 삭제를 진행한다.**

```bash
# 1. 미리보기 (dry-run)
aibris prune --dry-run

# 2. 7일 이상 된 것만 정리
aibris prune --age 7d

# 3. 특정 도구만
aibris prune --tool codex --age 14d

# 4. 하나씩 확인하며 정리
aibris prune --interactive

# 5. 강제 정리 (확인 생략)
aibris prune --force --age 30d

# 6. 전체 도구 대상
aibris prune --all --age 7d
```

## 주의

- `--dry-run` 없이 실행하면 confirm 프롬프트가 뜬다
- `--force`로만 confirm 생략 가능
- 절대 경로나 시스템 경로는 삭제하지 않는다
- 기본 `--age`는 168h (7일)
