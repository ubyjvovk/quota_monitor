# Tiger Team state

Written for a cold-start PM who has read nothing else. Keep it current: update
after every review cycle and before ending any session, then commit.

## Mission
<what we are building, for whom, and what "done" means overall>

## Configuration notes
<test command quirks, branch name, worker fleet composition, anything a worker
or future PM must know about this repo>

## Decision log (append-only)
- <date> — <decision> — <why>

## Board snapshot
<updated at checkpoints: date, lane counts, notable accepts/rejects>

## Next actions
- <ordered list the resuming PM executes>

## How to resume
1. Read this file.
2. `bash .tigerteam/scripts/board-status.sh`
3. Process review/ first (merge accepted ticket branches), then blocked/
   (answer questions, move to todo/).
4. `git worktree list` — tigerteam/* entries are unmerged ticket branches.
5. Relaunch workers: `tigerteam up` (or `tigerteam worker run <worker> --once`
   to smoke one worker first).
6. Continue planning from Next actions.
