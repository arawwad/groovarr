# Chat Execution Direction

This document replaces the old turn-loop refactor plan.

That refactor is already applied in code:
- the server has a canonical `Turn`
- the live chat path is staged
- session memory is centralized
- turn/archive inspection exists

The next problem is not "how do we add a turn loop?"
The next problem is "what is the right boundary between model interpretation and server execution?"

## Decision

Groovarr should stay:
- LLM-first for interpretation
- server-owned for state, validation, and execution

Groovarr should not keep expanding broad deterministic routing as the main way to compensate for weak agent/tool behavior.

## What We Believe Now

### 1. The model should own language understanding

The model is better than server-side heuristics at:
- typos
- paraphrases
- mixed intents
- referential follow-ups
- fuzzy language like mood, vibe, and taste framing

We should not rebuild a parallel NLU layer in Go through route sprawl.

### 2. The server should own grounded execution

The server should remain authoritative for:
- session state
- prior result sets
- pending actions
- approvals
- destructive operations
- final tool validation

The model may choose an action, but the server decides whether that action is legal, grounded, and executable.

### 3. Deterministic execution still has a narrow place

Deterministic handling is still appropriate for:
- approvals and pending-action application
- destructive flows like cleanup/apply/remove
- exact application of known cached selections
- explicit workflow continuations where the server already owns the state

Deterministic routing is not the right default answer for broad conversational behavior.

## Target Architecture

The intended shape is:

1. `normalizer`
   - LLM turns raw language into structured turn state
2. `audit / repair`
   - server validates, repairs, and constrains that structured state
3. `planner`
   - decide between deterministic execution, tool/agent path, or clarification
4. `execution`
   - server executes typed operations against trusted state and tools
5. `responder`
   - model or deterministic renderer produces the user-facing reply

The important part is that the model owns interpretation, while the server owns enforcement.

## Main Design Rules

### Strong typed turn state

`Turn` remains the shared object across stages.

The server should prefer:
- typed turn fields
- typed session artifacts
- typed execution requests

It should avoid:
- ad hoc prompt-only state
- duplicated request-shaped contracts
- route-specific hidden assumptions

### Typed session artifacts

Prior results should be represented as explicit server artifacts, not just implicit text context.

Examples:
- prior album result set
- prior playlist plan
- prior cleanup preview
- prior recent-listening summary
- prior selected/focused item

The model should be able to refer to these artifacts through structured turn state. The server should resolve them, not guess from raw wording alone.

### Tool calls must be repairable

The current weak point is not only interpretation. It is also the agent/tool contract.

We should assume the model will sometimes emit:
- unsupported arg names
- partially correct filters
- invalid combinations
- empty placeholders that should be dropped

So the server should add a stricter repair/validation layer before execution.

That layer should:
- drop unsupported args
- translate common aliases into canonical tool args
- reject unsafe or incoherent combinations
- preserve audit/debug traces of what changed

### Audit before execution or final response

We should add a lightweight audit step that can:
- verify scope
- verify reference grounding
- verify tool compatibility
- verify that the chosen path matches known session state
- catch obviously weak final responses before sending them

This is the right place to harden the system, not by pushing more user-language handling into deterministic routes.

## Near-Term Work

### 1. Add the audit / repair stage

This is the next architectural step.

Initial responsibilities:
- normalize tool args into canonical shape
- reject unsupported args early
- repair common model mistakes where safe
- preserve audit context on the `Turn`

### 2. Tighten the agent-tool contract

Focus on the tools the agent uses in follow-ups:
- album lookup / album filtering
- artist listening stats
- playlist planning and preview
- cleanup / apply flows

The goal is not more tools. The goal is fewer ambiguous, easier-to-repair tools.

### 3. Expose prior results as explicit structured state

Follow-ups like:
- "which of those have I played recently?"
- "from those, give me three to revisit"
- "clean those from lidarr"

should bind through typed server state first, not through fragile textual reconstruction.

### 4. Shrink deterministic route surface over time

Keep deterministic routes where they are clearly justified.

Do not add new deterministic routes just because the agent made a weak tool call once.

If a route exists only to compensate for:
- bad tool schemas
- weak arg repair
- missing session artifacts

that route is probably technical debt, not architecture.

## What To Avoid

- do not reintroduce regex-heavy intent handling as the main chat strategy
- do not let the model execute destructive actions directly
- do not hide workflow state only in prompt text
- do not expand route-specific execution logic when the real issue is tool-contract weakness

## Practical Standard

When deciding whether to add deterministic logic or improve the agent/tool layer, ask:

1. Is this flow destructive, approval-gated, or explicitly bound to server-owned workflow state?
   - If yes, deterministic handling may be correct.
2. Is this flow mainly about interpreting messy user language?
   - If yes, keep it model-driven.
3. Is the current failure really an interpretation failure, or is it an invalid tool-call / weak repair failure?
   - If it is a tool-contract failure, fix the contract.

## Status

Current state:
- turn-loop refactor: applied
- canonical `Turn`: in production path
- centralized session memory: in production path

Current direction:
- do not continue treating "more deterministic routes" as the default fix
- move next into audit, tool repair, and typed prior-result binding
