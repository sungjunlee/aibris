# aibris (AI + debris)

AI 코딩 도구(Codex CLI, Claude Code, Cursor 등)가 남긴 작업 잔해(worktree)를
탐지하고 정리하는 CLI 도구.

```bash
brew install sungjunlee/tap/aibris
# or
go install github.com/sungjunlee/aibris@latest

aibris scan                  # 스캔
aibris prune --age 7d        # 7일 지난 것 정리
aibris prune --dry-run       # 미리보기
```
