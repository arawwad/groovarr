# Auditor Fixes - Addressing Test Failures

## Problem Analysis

The initial auditor implementation was **too strict**, causing these failures:

1. **Count questions** ("How many Pink Floyd albums...") - Rejected instead of going to agent/tools
2. **"Do I have X" questions** - Rejected instead of using tools
3. **Followup clarifications** - Rejected valid followups that should go to agent
4. **Tool compatibility** - Rejected turns with empty execution fields that should use agent path

## Root Cause

The auditor was treating **all turns** as if they were going through deterministic execution, when many should fall through to the agent path.

## Fixes Applied

### 1. Tool Compatibility Check (`chat_auditor.go`)

**Before:**
```go
func (a *defaultTurnAuditor) verifyToolCompatibility(turn *Turn) bool {
    // Immediately checked set kind and operation
    if turn.Execution.SetKind != "" {
        // Rejected if not found
    }
}
```

**After:**
```go
func (a *defaultTurnAuditor) verifyToolCompatibility(turn *Turn) bool {
    // If no execution set kind or operation, this turn will go to agent - that's valid
    if turn.Execution.SetKind == "" && turn.Execution.Operation == "" {
        return true
    }
    // Only validate if deterministic execution is planned
}
```

### 2. Rejection Logic (`chat_auditor.go`)

**Before:**
```go
func (a *defaultTurnAuditor) shouldReject(turn *Turn, audit TurnAudit) bool {
    // Rejected if reference not grounded
    if turn.Normalized.FollowupMode != "none" && !audit.ReferenceGrounded {
        return true
    }
    // Rejected if tool compatibility failed
    if !audit.ToolCompatible {
        return true
    }
}
```

**After:**
```go
func (a *defaultTurnAuditor) shouldReject(turn *Turn, audit TurnAudit) bool {
    // Don't reject if this will go to agent path (no execution operation set)
    if turn.Execution.Operation == "" {
        return false
    }
    
    // Only reject ungrounded references if explicitly required
    if turn.Normalized.FollowupMode != "none" && turn.Normalized.FollowupMode != "" && !audit.ReferenceGrounded {
        if turn.Normalized.ReferenceTarget != "" && turn.Normalized.ReferenceTarget != "none" {
            return true
        }
    }
    
    // Only reject tool compatibility for deterministic execution
    if !audit.ToolCompatible && turn.Execution.SetKind != "" {
        return true
    }
}
```

### 3. Pipeline Behavior (`turn_pipeline.go`)

**Before:**
```go
if audit.Rejected {
    turn = turn.withOutcome(TurnOutcome{
        Response: fmt.Sprintf("I couldn't process that request: %s. Could you clarify?", audit.RejectionReason),
        Handled:  true,
    })
    return ChatResponse{Response: turn.Outcome.Response}, turn, nil
}
```

**After:**
```go
if audit.Rejected {
    // Log rejection for debugging
    log.Warn().
        Str("request_id", chatRequestIDFromContext(chatCtx)).
        Str("reason", audit.RejectionReason).
        Strs("warnings", audit.ValidationWarnings).
        Msg("Turn rejected by auditor")
    
    // Don't send generic rejection - fall through to agent instead
    // The agent can handle these cases better than a generic error
    turn = turn.completeDebugStage(StageAuditor, "rejected_fallthrough_to_agent")
    // Continue to agent path below
}
```

## Design Philosophy

### The Auditor's Role

**The auditor validates deterministic execution paths, NOT agent paths.**

```
User Input
    ↓
Normalizer
    ↓
Planner
    ↓
Resolver
    ↓
AUDITOR ← Only validates if going to deterministic executor
    ↓
├─→ Executor (deterministic) ← Auditor validates this
└─→ Agent (dynamic tools)     ← Auditor allows this
```

### When to Reject

**Reject only when:**
1. Deterministic execution is planned (`turn.Execution.Operation != ""`)
2. AND the execution would clearly fail (invalid tool, missing selection, etc.)

**Allow (fall through to agent) when:**
1. No execution operation set (agent will handle it)
2. Reference not grounded but no explicit requirement
3. Tool compatibility issues but no deterministic execution planned

### Why This Works

The agent can handle:
- Count questions → Uses `libraryFacetCounts` or `artistLibraryStats` tools
- "Do I have X" → Uses `artists` or `albums` tools
- Followup clarifications → Uses session context and tools dynamically
- Ambiguous requests → Interprets and uses appropriate tools

The auditor should only block **clearly invalid deterministic execution**, not **potentially valid agent requests**.

## Test Updates

Updated `chat_auditor_test.go` to reflect new behavior:

```go
{
    name: "followup without grounded reference but no execution",
    turn: &Turn{
        Normalized: TurnNormalized{
            FollowupMode:    "refine_previous",
            ReferenceTarget: "previous_results",
        },
        Reference: TurnReference{},
        Execution: TurnExecution{}, // No execution operation - will go to agent
    },
    expectValid:    true,  // Changed: now valid because it will go to agent
    expectRejected: false, // Changed: not rejected, falls through to agent
},
```

## Impact on Reported Failures

### ✅ Fixed: Count Questions
- "How many Pink Floyd albums..." → Now goes to agent → Uses tools → Returns count

### ✅ Fixed: "Do I Have X" Questions  
- "Do I have Heart-Shaped Box..." → Now goes to agent → Uses tools → Returns yes/no

### ✅ Fixed: Followup Clarifications
- "Which of those have I played recently?" → Now goes to agent → Uses session context + tools

### ✅ Fixed: Tool Incompatible Errors
- No longer immediately rejects with "tool_incompatible" → Falls through to agent

### ✅ Fixed: Clarification Loops
- Agent can now handle ambiguous requests instead of auditor rejecting them

## Monitoring

The auditor still logs rejections for debugging:

```
WARN Turn rejected by auditor 
  request_id=abc123 
  reason=tool_incompatible 
  warnings=[scope_mismatch]
```

But instead of returning an error, it falls through to the agent path, which is logged as:

```
DEBUG audit: rejected_fallthrough_to_agent
```

This allows us to:
1. Track what the auditor would have rejected
2. See if the agent successfully handled it
3. Identify patterns for future improvements

## Summary

The auditor is now **lenient by design**:
- Validates deterministic execution when planned
- Allows agent path for everything else
- Logs rejections but doesn't block agent fallback
- Follows the principle: "Model owns interpretation, Server owns execution"

The agent is the interpretation layer - let it interpret. The auditor is the execution validator - only validate when executing deterministically.
