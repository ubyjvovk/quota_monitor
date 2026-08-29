# Agent orientation

<!-- Maintained by the Tiger Team PM. Workers and agent CLIs read this first;
     workers never edit it. Keep it short, current, and written for a capable
     stranger with zero context. Update it whenever accepted work changes the
     map. -->

## What this project is
<one paragraph: purpose, runtime, who consumes it>

## Layout
<dir → purpose, one line each; only what matters>

## Conventions
<naming, error handling, test style, docstring expectations — the things a
stranger would get subtly wrong>

## Config
- Project config is root `tigerteam.toml` (optional `~/.tigerteam.toml` for
  machine-local facts). Full chain: `references/config-reference.md`.
- The runner injects `TIGERTEAM_TEST_CMD` from resolved `test_cmd` into every
  worker environment; `run-tests.sh` uses that env var first, then
  `tigerteam config get test_cmd`.

## Commands
- Tests: `bash .tigerteam/scripts/run-tests.sh` (the ONLY way to run tests;
  needs `TIGERTEAM_TEST_CMD` or `test_cmd` in `tigerteam.toml`)
- <build / lint / run, if any>

## Landmarks & gotchas
<the 3–5 things that repeatedly surprise newcomers: quirky modules, load-time
side effects, files that look editable but are generated, etc.>
