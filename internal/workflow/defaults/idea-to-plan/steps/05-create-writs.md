# Create Writs

Turn the plan into concrete writs using `sol writ create`.

For each item in the plan:

```
sol writ create --world={{world}} \
  --title="<concise title>" \
  --description="<what to implement, context, acceptance criteria>"
```

Guidelines:
- Each writ should be independently implementable by a single agent
- Titles should be action-oriented: "Add ...", "Implement ...", "Update ..."
- Descriptions should include enough context for an agent to work without
  asking clarifying questions — mention relevant files, patterns, and constraints
- If items have ordering dependencies, note them in the descriptions
- Keep items focused — prefer several small writs over one large writ

Record the IDs of all created writs.

When all writs are created, advance:
`sol workflow advance --world={{world}} --agent=$SOL_AGENT`
