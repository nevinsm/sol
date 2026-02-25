# gt — Multi-Agent Orchestration

A production-ready system for coordinating concurrent AI coding agents.

## Quick Start (Loop 0)

```bash
# Build
make build

# Create a rig and an agent
export GT_HOME=~/gt
gt agent create Toast --rig=myrig

# Create a work item
gt store create --db=myrig --title="Implement feature X" --description="..."

# Dispatch to the agent
gt sling <work-item-id> myrig

# Watch the agent work
gt session attach gt-myrig-Toast

# Check status
gt store list --db=myrig
gt session list
```

## Architecture

See the design documents in the original Gastown repository for the full
target architecture.

## Current Status

Loop 0: Single agent dispatch. One operator, one agent, one work item at
a time. Crash recovery via hook durability.
