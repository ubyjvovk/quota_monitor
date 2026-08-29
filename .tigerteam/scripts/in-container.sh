#!/usr/bin/env bash
# Canonical per-ticket container invocation — used by worker.sh (container
# mode) and by the PM for in-environment verification:
#   in-container.sh <workdir> [docker-args...] -- <cmd...>
#
# Invariants (see docker/README.md):
#   - project mounted at the SAME absolute path as on the host (git worktree
#     metadata stores absolute paths; identical paths keep both sides valid)
#   - runs as the host uid/gid (no root-owned files on the mount)
#   - HOME inside = the mounted credentials bundle (WORKER_SECRETS_DIR) — the
#     legacy auth mount; migrated engines (claude/codex/grok/kimi) get their
#     agent state from WORKER_AGENTS_HOME and the per-engine vars below.
#     HOME stays /tigerteam-home in every mode.
#   - cache volumes make uv/npm installs near-instant across tickets
# Env: WORKER_IMAGE (required), WORKER_SECRETS_DIR,
#      WORKER_AGENTS_HOME (host agent-state dir, mounted read-write at the
#        same path; points AGENTS_HOME + CLAUDE_CONFIG_DIR + CODEX_HOME +
#        GROK_HOME + KIMI_CODE_HOME into ${WORKER_AGENTS_HOME}/<engine>),
#      TIGERTEAM_CNAME, WORKER_CTR_MEMORY (3g), WORKER_CTR_CPUS (2),
#      WORKER_DOCKER_ARGS,
#      TIGERTEAM_RW_ROOT (PM-only: set to 1 to restore a single rw root
#        mount for merged-tree verification whose workdir IS the root;
#        the runner never sets it).
set -u
cd "$(dirname "$0")/../.." || exit 1
ROOT="$(pwd)"
WD="${1:?usage: in-container.sh <workdir> [docker-args...] -- <cmd...>}"; shift
case "$WD" in /*) : ;; *) WD="$ROOT/$WD" ;; esac
DARGS=()
while [ $# -gt 0 ] && [ "$1" != "--" ]; do DARGS+=("$1"); shift; done
[ "${1:-}" = "--" ] && shift
[ $# -gt 0 ] || { echo "in-container: no command given" >&2; exit 64; }
: "${WORKER_IMAGE:?in-container: WORKER_IMAGE not set}"

HOMEDIR="/tigerteam-home"
SECRETS=()
[ -n "${WORKER_SECRETS_DIR:-}" ] && SECRETS=(-v "${WORKER_SECRETS_DIR}:${HOMEDIR}")

# Agent-state mount: same-path RW so absolute paths recorded inside the state
# dirs stay valid on both sides and CLIs can refresh OAuth tokens in place.
# Composes with (never replaces) the legacy secrets bundle above.
AGENTS=()
if [ -n "${WORKER_AGENTS_HOME:-}" ]; then
  AGENTS=(-v "${WORKER_AGENTS_HOME}:${WORKER_AGENTS_HOME}" \
    -e "AGENTS_HOME=${WORKER_AGENTS_HOME}" \
    -e "CLAUDE_CONFIG_DIR=${WORKER_AGENTS_HOME}/claude" \
    -e "CODEX_HOME=${WORKER_AGENTS_HOME}/codex" \
    -e "GROK_HOME=${WORKER_AGENTS_HOME}/grok" \
    -e "KIMI_CODE_HOME=${WORKER_AGENTS_HOME}/kimi")
fi

# Mask project-root .env inside the container so workers cannot read secrets
# that only the host/supervisor should resolve. Per-lane credentials still
# arrive via env_passthrough (docker bare -e NAME). Nested bind at
# $ROOT/.env overlays the broader $ROOT mount by path depth.
ENV_MASK=()
[ -f "$ROOT/.env" ] && ENV_MASK=(-v "/dev/null:${ROOT}/.env:ro")

# Root is read-only by default; rw is granted only on .git and .tigerteam
# (nested binds override by path depth — same mechanism as the .env mask).
# TIGERTEAM_RW_ROOT=1 is PM-only: restores the old single rw root mount
# for merged-tree verification runs whose workdir IS the root. The
# runner never sets it.
ROOT_MOUNTS=()
if [ "${TIGERTEAM_RW_ROOT:-}" = "1" ]; then
  ROOT_MOUNTS=(-v "$ROOT:$ROOT")
else
  ROOT_MOUNTS=(-v "$ROOT:$ROOT:ro")
  [ -d "$ROOT/.git" ] && ROOT_MOUNTS+=(-v "$ROOT/.git:$ROOT/.git")
  [ -d "$ROOT/.tigerteam" ] && ROOT_MOUNTS+=(-v "$ROOT/.tigerteam:$ROOT/.tigerteam")
fi

# tini as PID 1 reaps reparented zombies; engine CLIs are not init.
# shellcheck disable=SC2086
exec docker run --rm --init \
  ${TIGERTEAM_CNAME:+--name "$TIGERTEAM_CNAME"} \
  --user "$(id -u):$(id -g)" \
  ${ROOT_MOUNTS[@]+"${ROOT_MOUNTS[@]}"} \
  -w "$WD" \
  -e "HOME=$HOMEDIR" \
  -e "GIT_AUTHOR_NAME=${WORKER_ID:-tigerteam-worker}" \
  -e "GIT_AUTHOR_EMAIL=${WORKER_ID:-tigerteam-worker}@tigerteam.local" \
  -e "GIT_COMMITTER_NAME=${WORKER_ID:-tigerteam-worker}" \
  -e "GIT_COMMITTER_EMAIL=${WORKER_ID:-tigerteam-worker}@tigerteam.local" \
  ${TIGERTEAM_TEST_CMD:+-e "TIGERTEAM_TEST_CMD"} \
  ${SECRETS[@]+"${SECRETS[@]}"} \
  ${AGENTS[@]+"${AGENTS[@]}"} \
  ${ENV_MASK[@]+"${ENV_MASK[@]}"} \
  -v tigerteam-cache-uv:/cache/uv \
  -v tigerteam-cache-npm:/cache/npm \
  --memory "${WORKER_CTR_MEMORY:-3g}" \
  --cpus "${WORKER_CTR_CPUS:-2}" \
  --pids-limit 512 \
  ${WORKER_DOCKER_ARGS:-} \
  ${DARGS[@]+"${DARGS[@]}"} \
  "$WORKER_IMAGE" \
  "$@"
