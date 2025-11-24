# xuanwu-mahjong-game x /xf/implement Override

> Active when `project_profile: xuanwu-mahjong-game`.

## Override: code-generator owns implementation

Implementation is delegated to the `code-generator` skill. The command layer
does not execute individual task cards itself and does not pre-read target code.

### Keep From The Generic Flow

- Preconditions remain mandatory: `constitution.md`,
  `xspec/changes/mahjong-game/task-handoff.json`, the task shard files declared
  by that handoff, and `change.yaml.phase in {task, implement}`.
- Do not require `xspec/changes/mahjong-game/task.md`; this profile uses
  sharded Coverage Tasks with `task-handoff.json` as the implementation entry.
- Step 0 profile loading remains unchanged.
- On first execution, set `change.yaml.phase = implement`; user-provided task
  range parameters still apply.
- Integration tasks are skipped because this profile has a single
  `mahjong-game` module.

### Replace Validation Command Detection

Use the checker workflow inside `code-generator`:

- Client: `client/assets/<game_id>/` plus
  `client/fgui_game/<fgui_project_name>/`.
- Server: `server/game_server/Logic/mahjong_server/src/games/<server_module_name>/`.
- Specification source: client/server xuanwu-mahjong-game `spec-index.md` and
  referenced `modules/*.md`.

### Replace Task Execution

Read `.opencode/skills/code-generator/SKILL.md` and execute:

```text
code-generator --profile xuanwu-mahjong-game --module mahjong-game
```

Inputs:

- `xspec/changes/mahjong-game/task-handoff.json`
- `xwRef/xuanwu/xuanwu-mahjong-game/impl/bundle.yaml`
- `xwRef/xuanwu/frameworks/client/xuanwu-mahjong-game/spec-index.md`
- `xwRef/xuanwu/frameworks/server/xuanwu-mahjong-game/spec-index.md`
- `client/assets/<game_id>/SKELETON_CONTRACT.md` when present

Client implementation requirements:

- Client IU agents must read `SKELETON_CONTRACT.md` before editing
  skeleton-derived files.
- Preserve the minimal loop order:
  `protocol/data -> Cmd -> Controller -> View -> peripheral`.
- The base deal/draw/play-power/discard/end chain belongs to skeleton-derived
  client files.
- Replay, reconnect, bot, and settlement-authority behavior belongs to
  peripheral tasks and must not be mixed into the base chain.

Outputs:

- Client code: `client/assets/<game_id>/`
- FGUI project/output: `client/fgui_game/<fgui_project_name>/`
- Server code: `server/game_server/Logic/mahjong_server/src/games/<server_module_name>/`
- Implementation report: `xspec/changes/mahjong-game/impl-report.md`

### Replace Phase And Final Verification

Verification is performed by the skill checkers. The command layer reads
`impl-report.md` and confirms:

- All required IU are complete.
- No unclassified or unresolved implementation finding remains. Pre-existing
  task-level blocked items may remain only when `impl-report.md` carries their
  freeze_id, reason, and unblock action forward without scheduling them into an
  executable IU.
- `client-impl-checker` and `server-impl-checker` have final PASS results, or
  any failure is explicitly marked blocked.

### Completion Report

```text
Implementation complete (xuanwu-mahjong-game)

Module: mahjong-game
Code output:
  Client: client/assets/<game_id>, client/fgui_game/<fgui_project_name>
  Server: server/game_server/Logic/mahjong_server/src/games/<server_module_name>

Task progress: X/Y
DAG waves: X
Self-check:
  client-impl-checker: passed / failed
  server-impl-checker: passed / failed
Blocked items: X
Implementation report: xspec/changes/mahjong-game/impl-report.md
```
