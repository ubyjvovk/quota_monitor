# Incident: misattributing the transient `.tigerteam/STOP` drain artifact

**Author:** PM (Claude) · **Date:** 2026-08-30 · **Times it happened:** 2 (both this session)

## What I claimed

Twice, right after a background `tigerteam up` task was killed, I ran
`tigerteam status`, saw `!! STOP file present — workers will not claim`, and
told the user some variant of:

> "The fleet has been halted from outside this session: a `.tigerteam/STOP`
> file appeared (I didn't create it)… I'm treating that as your deliberate
> halt."

The user's response the second time: *"you hallucinated a STOP file that
doesn't exist."*

## What actually happened

The file was **real at the moment I checked** — `tigerteam status` genuinely
printed the warning and `ls` genuinely listed `.tigerteam/STOP  0B`. It was
**gone** by the time the user looked (and when I re-ran `ls` afterwards:
`No such file or directory`). Nothing was fabricated in the tool output. The
failure was my **causal story**, not my perception.

The mechanism, confirmed in the tigerteam source (`fleet.py`):

- I run the supervisor as a background task: `tigerteam up`.
- Killing that background task sends **SIGTERM** → `_handle_signal` →
  `request_stop()` (`fleet.py` ~590).
- Graceful shutdown calls **`_mark_stop()`** (`fleet.py:573`): *"Create the
  STOP marker unless one already belonged to the board."* → it `touch`es
  `.tigerteam/STOP` so workers stop claiming while active tickets drain.
- When drain finishes, **`_clear_stop()`** (`fleet.py:583`) removes **only the
  STOP file this supervisor created** (`_stopfile_created` guard).

So `.tigerteam/STOP` is a **transient artifact of my own supervisor shutting
down**. There is a short window — from "kill signal received" to "drain
complete" — where the file exists. Both times I received the task-killed
notification and *immediately* ran `tigerteam status`, landing inside that
window. By the next check the supervisor had cleaned it up.

## The actual failure mode

Not hallucination (inventing tokens). It was **false causal attribution of a
self-inflicted, expected artifact**, delivered with unwarranted certainty:

1. **Wrong agent.** I attributed the STOP file to "outside this session" / the
   user, when the cause was *my own* background supervisor responding to *its
   own* task being killed. The one event that immediately preceded it — the
   `tigerteam up` task-killed notification — was the cause, and I had it in
   hand.
2. **False certainty.** "I didn't create it" was stated as fact. I did, at one
   remove: my supervisor did, because I launched and then (the harness) killed
   it.
3. **Snapshot vs. lifecycle.** I treated "file present in this one `status`"
   as a durable state ("the fleet has been halted") instead of a possibly
   transient one. I did not re-check.
4. **Pattern-lock made it worse.** The second time, "this is the second time"
   reinforced the earlier wrong narrative instead of prompting me to
   investigate why a STOP keeps appearing exactly when my supervisor dies.

## Correct handling next time

- **A killed `tigerteam up` task + a STOP file that appears at the same moment
  = normal graceful drain, self-inflicted.** Expect it; do not narrate it as an
  external halt.
- Before claiming *who* did something, name the evidence. The preceding
  task-killed notification is the cause; there is no need to invent an external
  actor.
- Distinguish **"present right now"** from **"standing state."** For a file
  known to be transient (drain markers), re-check (`ls` / a second `status`)
  before drawing conclusions, or wait for the supervisor's `supervisor stopped`
  line in its own output.
- Report at the right confidence: *"my supervisor task was killed; it's
  draining and left a transient STOP — it should clear on its own,"* not
  *"someone halted the fleet."*
- If the board must actually resume and a stale STOP remains after the
  supervisor is fully down, `rm .tigerteam/STOP` is safe — but only after
  confirming no supervisor is still draining (`pgrep -f 'tigerteam up'`).

## One-line rule

A `.tigerteam/STOP` that shows up the instant my own `tigerteam up` task is
killed is that supervisor's drain marker, not an external halt — verify the
preceding event before attributing cause, and re-check transient files before
calling them state.
