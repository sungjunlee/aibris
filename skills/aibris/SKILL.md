---
name: aibris
description: Scan and clean AI coding tool debris — worktrees, node_modules, build caches. Use for disk cleanup, storage management, AI tool maintenance. Triggers: aibris, disk cleanup, worktree 정리, 캐시 정리, 디스크 공간, storage full, 코딩 잔해 정리, AI debris cleanup.
---

# aibris — AI Development Debris Cleaner

AI 코딩 도구가 남긴 작업 잔해(worktree, node_modules, build cache)를 탐지하고 정리하는 CLI.
Worktree는 특정 도구 목록보다 `$HOME` 아래 worktree convention을 발견하고 `.git` metadata로 검증한다.

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
- worktree는 `source`, `project`, `status`로 그룹핑하고 `risk`, `reason`도 함께 본다
- scan의 `active`는 상위 Git metadata가 연결되어 있다는 뜻이지 최근 사용 중이라는 뜻이 아니다. classic clean에서는 제외되므로 일반 정리 제안에서는 `orphaned`를 우선한다
- 검증된 **active** worktree가 큰 비중을 차지하면 `aibris clean --dry-run` 경로를 우선 제안한다. 도구가 Codex가 아니어도 같다. 필터가 없고 검증된 active cleanup unit이 256 MB 이상이거나 3개 이상이면, 추천이 0개여도 기본 guided review가 열린다. 등록된 session-activity reader는 Codex만 있다. reader가 없는 도구는 그 이유만으로 자동 추천되지 않고, dirty/recent/retention 같은 다른 hard lock이 없으면 `reviewable` + `activity_source_not_registered`가 된다.
- guided 결과는 `recommended`(기본 선택), `reviewable`(사용자 선택 가능), `locked`(선택 불가)로 해석한다. scan JSON의 project label만으로 active 항목의 안전을 추정하지 않는다
- `plain-dir` / review-only owner는 절대 선택·삭제·strip 대상이 아니다. metadata를 검사하라고만 안내한다. 빈 leftover member와 등록된 sidecar(`.orca-worktree-trash`)는 mixed marker가 아니다.
- 삭제하면 안 되는 **protected active** 체크아웃에서 regenerable subtree만 걷어내려면 `aibris clean --strip --dry-run`을 제안한다. strip은 세 번째 disposition이다. unit을 지우지 않는다. cwd가 unit 또는 그 subtree 안이면 거부한다.
- Codex activity 판단은 session metadata, cwd, timestamp만 사용한다. 대화 본문은 읽거나 요약하지 않는다.
- `by_category`에 없는 카테고리는 출력에서 제외한다
- Docker가 있으면 별도 섹션으로 추가한다

```markdown
📦 전체: {total_count}개 항목 | {total_size}

[워크트리] {count}개 | {size}
  • {source}/{project}: {count}개 ({size}) — {status}, 최신 {relative_time}, 가장 오래된 것 {relative_time}

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
> active worktree는 classic clean에서 기본 제외된다. guided review는
> 모든 도구의 **active** unit을 받는다. 별도 증거 정책을 통과한 unit만
> 추천한다. `--include-active-worktrees`는 classic 경로에서만 의도적으로
> 포함할 때 쓴다. non-Codex active의 첫 제안은 guided review이지
> `--include-active-worktrees`가 아니다.

실제 프로젝트명과 수치를 위 템플릿에 채워서 보여준다.

### Step 3: 질문

**가장 큰 항목부터 우선 질문한다.** 카테고리 순서보다 전체 크기 기준으로 정렬한다.
아래 질문 형식을 참고하되, 카테고리 순서가 아닌 크기 순서로 질문한다:

- **worktree(orphaned)**: "{source}/{project} orphaned 워크트리가 {count}개 ({size}) 있습니다. 상위 repo metadata가 없어서 정리 후보입니다. 지워도 될까요?"
- **worktree(active)**: "{source}/{project}에 연결된 active 워크트리가 {count}개 ({size}) 있습니다. `active`는 최근 사용 의미가 아닙니다. `aibris clean --dry-run`의 recommended/reviewable/locked 근거를 확인해야 합니다. guided preview를 열까요?"
- **worktree(plain-dir)**: "{source}/{project} review-only 항목이 {count}개 ({size}) 있습니다. valid metadata가 없거나 mixed marker라 삭제·strip 후보가 아닙니다. 지우지 마세요."
- **worktree(protected, strip)**: "보호된 active 체크아웃 안에 regenerable subtree가 {size} 있습니다. 체크아웃은 유지하고 `aibris clean --strip --dry-run`으로만 걷어낼 수 있습니다. 그 워크트리 안에서 실행 중이면 cwd 거부로 건너뜁니다."
- **node_modules**: "{path}의 node_modules가 {size}인데, 이 프로젝트 아직 사용 중인가요?"
- **build-cache**: "{cache_name} 캐시가 {size} 쌓였는데, 지우시겠어요? (단, 다음 빌드 시 다시 다운로드)"
- **pip-cache**: "{cache_name} 캐시 {size} — 지우시겠어요?"
- **docker**: "Docker 이미지/캐시가 {size} 쌓였습니다. `docker system prune -a`로 정리할까요? (단, 실행 중인 컨테이너에 영향 없음, 이미지는 재다운로드 필요)"

**모두 거절 시**: "모두 유지합니다. `/clear`로 마무리할게요." 라고 안내하고 `/clear` 권장 후 중단.

### Step 4: 실행

**규칙: 절대 --dry-run 없이 실제 삭제를 실행하지 않는다.**

사용자가 특정 범위 삭제를 승인하면 먼저 `--dry-run`으로 대상 목록을 보여준다.
이때 preview와 실제 실행의 selector 계약은 불변이다:

- 승인받은 `--category`, `--tool`, 반복 가능한 `--root`, `--age` 값은 모두
  동일하게 유지한다
- `--guide`, `--no-guide`, `--risky`, `--include-active-worktrees`,
  `--interactive`, `--force`, `--strip` 같은 적용 가능한 routing/safety flag도
  동일하게 유지한다
- 실제 실행에서는 preview 명령에서 `--dry-run`만 제거한다
- scoped preview 뒤에 plain `aibris clean`을 실행해서는 안 된다

예를 들어 scoped cleanup은 다음 두 명령처럼 selector가 정확히 대응해야 한다:

```bash
aibris clean --no-guide --root ~/path/to/project --category node_modules --tool node_modules --age 30d --dry-run
aibris clean --no-guide --root ~/path/to/project --category node_modules --tool node_modules --age 30d
```

아래 plain-command guided flow는 사용자가 selector나 safety flag 없는
guided 정리를 승인한 경우에만 사용하는 별도 분기다:

```bash
# evidence-based 추천 기본 선택 + 번호 토글 + 삭제 없는 미리보기
aibris clean --dry-run
```

guided header의 기본 정책은 `idle>3d, recent<6h locked, keep=3/repo,
min-size=256MB`다. 판정 순서는 다음과 같다:

1. 현재 디렉토리 포함, dirty/untracked, Git/activity evidence unavailable,
   named ref에서 도달 불가능한 detached HEAD, 6시간 이내 활동은 `locked`.
2. canonical Git common-dir별 최신 3개는 `reviewable`.
3. 3일보다 젊거나 256 MB 미만인 안전 unit은 `reviewable`.
4. 등록된 session-activity reader가 없는 도구는 `reviewable`.
5. local `refs/remotes/origin/HEAD` 대비 unique 콘텐츠가 있거나 uniqueness가
   `unknown`이면 `reviewable` (`unique_commits_not_in_default` /
   `merge_evidence_unknown`). GitHub API는 쓰지 않는다. 스쿼시 머지는
   merge-tree 트리 동등으로 판정하며 `git cherry`는 쓰지 않는다.
6. 나머지는 `recommended`. merged라는 이유만으로 승격하지 않는다.

scan의 `active`는 상위 gitdir 생존이지 최근 사용이나 머지 여부가 아니다.
upstream 미설정/삭제는 설명 metadata일 뿐 단독 잠금 사유가 아니다. 로컬
branch ref가 있거나 detached HEAD가 named local/remote ref에서 도달 가능하면
committed state는 recoverable하다. multi-member unit은 물리 크기를 한 번만
세되 모든 member가 hard safety를 통과해야 한다. uniqueness는 member 하나라도
unique/`unknown`이면 유닛 전체를 추천에서 내린다.

사용자가 guided preview의 선택 항목을 확인하고 실제 삭제를 재승인하면 `--dry-run`을 제거한다:

```bash
# 사용자 재승인 후 실제 실행. 삭제 전 최종 confirm은 그대로 유지됨
aibris clean
```

```bash
# guided minimum idle age만 7일로 바꾼 미리보기
aibris clean --guide --age 7d --dry-run
```

사용자가 미리보기를 확인하고 실제 삭제를 재승인하면 `--dry-run`을 제거한다:

```bash
# 2. 사용자 재승인 후 실제 실행
aibris clean --guide --age 7d
```

`--age`는 guided에서는 minimum idle age만 바꾼다. 6시간 recent hard lock과
repository별 최신 3개 retention은 그대로다. prompt 안에서도 `age 7d`, `+`,
`-`, `[`, `]`로 같은 값만 재계획할 수 있고, selectable row에 대한 사용자
선택 override는 유지된다.

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
aibris scan --root ~/.codex --json  # 특정 HOME 하위 경로만 스캔
```

기본 스캔 root는 `$HOME`이다. `--root`는 여러 번 지정할 수 있고, 반드시
`$HOME` 아래로 해석되어야 한다. `/`, `/tmp`, symlink escape는 거부된다.

### clean

```bash
# 미리보기 (항상 먼저 실행)
aibris clean --dry-run
aibris clean --no-guide --dry-run      # guided review 대신 classic audit 강제

# category 필터 (여러 개: 쉼표)
aibris clean --category worktree --dry-run
aibris clean --category node_modules,build-cache --dry-run

# tool 필터
aibris clean --tool codex --dry-run

# guided worktree cleanup 강제 실행 (모든 도구의 active unit)
aibris clean --guide --dry-run

# 보호된 worktree에서 regenerable subtree만 제거 (unit은 유지)
aibris clean --strip --dry-run

# category + tool AND 조합
aibris clean --category worktree --tool codex --dry-run

# scan root 제한
aibris clean --root ~/path/to/project --category node_modules --dry-run

# age: guided에서는 minimum idle, classic에서는 필터
aibris clean --age 3d --dry-run        # guided 기본 minimum idle 3일
aibris clean --age 30d --dry-run       # 30일 이상
aibris clean --age 1mo --dry-run       # 30일 이상 (month shorthand)
aibris clean --age 1y --dry-run        # 365일 이상
aibris clean --age 1h --dry-run        # 1시간 이상 (매우 공격적, 경고 출력)

# interactive (항목별로 y/N 확인 후 실제 삭제)
aibris clean --interactive

# risky 카테고리 포함 (ai-logs, windsurf 등; Cursor project store 제외)
aibris clean --risky --dry-run

# classic 경로에서 active worktree까지 포함
aibris clean --category worktree --include-active-worktrees --dry-run

# 실제 실행 (사용자 재승인 후)
aibris clean --category node_modules
# confirm 생략 (주의: 사용자 동의 없이 삭제)
# aibris clean --force --category node_modules
```

### categories

| Category | clean 기본? | 설명 | tool 목록 |
|----------|-----------|------|-----------|
| `worktree` | ✅ orphaned만 | `$HOME` worktree convention. active는 기본 제외 | codex, claude, unknown + source |
| `node_modules` | ✅ 안전 | `$HOME` scan root 아래 npm project dependencies | node_modules |
| `build-cache` | ✅ 안전 | Go/Xcode/Gradle/npm/Cargo caches | build-cache |
| `other-cache` | ✅ 안전 | Python/uv pip caches | pip-cache |
| `agent-state` | ✅ recorded cwd 부재로 orphaned 분류; classic `--age`는 적용되지 않음; `--agent-state-grace` 경과 후 기본 선택 (0으로 해제) | 분류는 증명 기반. idle age는 store 내부 최신 mtime 기준. grace window 안의 항목은 기본 선택에서 제외되며 후보 row로도 나오지 않으므로, 지우려면 `--agent-state-grace 0`(또는 더 짧은 값)으로 재실행하거나 grace 경과를 기다린다. live/undetermined는 항상 보호 | claude, cursor |
| `ai-logs` | 🚫 `--risky` 필요 | AI tool session logs, file history, archived sessions | ai-logs, windsurf |

## 주의사항

- 절대 경로나 시스템 경로(`/`, `/usr`, `/etc` 등)는 삭제 방지되어 있음
- 기본 스캔 root는 `$HOME`. `--root`로 HOME 하위 경로만 좁힐 수 있음
- worktree는 `$HOME` 아래 `worktrees`, `worktree`, `worktree-*`, `worktrees-*` convention을 찾고 direct/nested `.git` 파일로 검증함
- hidden owner 디렉토리(`.codex`, `.somename` 등)는 worktree source일 수 있으므로 일반적으로 스캔 대상임
- 전체 `$HOME`을 무제한 재귀 탐색하지 않고, scan root에서 얕은 컨테이너 depth 안의 convention을 찾음
- `$HOME` 스캔 중 `.Trash`, `Library`, `Applications`, `Pictures`, `Movies`, `Music`, `.git`, `vendor`, 중첩 `node_modules` 등은 가지치기함
- `Desktop`, `Downloads`는 기본 스캔에 포함됨
- classic 기본 `--age`는 `7d`; guided 기본 minimum idle age는 `3d`다
- 필터 없는 `aibris clean --dry-run`은 검증된 active worktree pressure가 256 MB 이상이거나 3 unit 이상이면 추천 0개여도 guided review를 연다. `--no-guide`와 명시적 classic selector는 classic 경로를 강제하고, `--guide`는 모든 도구의 guided review를 명시적으로 강제한다
- `plain-dir`은 age/`--risky`/`--include-active-worktrees`와 무관하게 절대 정리 후보가 아니다
- `clean --strip`은 보호된 worktree의 regenerable subtree만 제거한다. unit·branch·uncommitted work는 유지한다. cwd가 그 unit 안이면 거부한다
- `--dry-run` 없이 실행하면 confirm 필요. `--force`는 confirm만 생략하며 locked row를 풀거나 `git worktree remove --force`로 전달되지 않는다
- classic에서는 active worktree가 기본 제외된다. `--include-active-worktrees`로 포함해도 Git hard safety 검사를 통과해야 한다
- active worktree는 실행 직전 모든 member의 repository/HEAD/dirty/ref를 재검사하고 Git-aware removal로 제거한다. branch ref와 parent `git worktree` metadata를 검증하며 실패 시 raw recursive deletion으로 fallback하지 않는다
- `go-build`, `npm`, `uv` 캐시는 가능하면 공식 command(`go clean -cache`, `npm cache clean --force`, `uv cache clean`)로 정리함
- command가 없으면 기존 safe path 삭제로 fallback하지만, command가 실행 후 실패하면 조용히 fallback하지 않음
- orphaned/일반 path target은 안전 검사 후 경로 삭제한다. active worktree는 non-forced Git-aware executor를 사용하고 branch ref는 삭제하지 않는다
- **ai-logs 계열**은 기본 clean에서 제외. 삭제하려면 `--risky` 필요

## 개발

개발 관련 사항은 `AGENTS.md` 참고.
