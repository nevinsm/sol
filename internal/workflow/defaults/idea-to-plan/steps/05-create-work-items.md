# Create Work Items

Turn the plan into concrete work items using `sol store create`.

For each item in the plan:

```
sol store create --world={{world}} \
  --title="<concise title>" \
  --description="<what to implement, context, acceptance criteria>"
```

Guidelines:
- Each work item should be independently implementable by a single agent
- Titles should be action-oriented: "Add ...", "Implement ...", "Update ..."
- Descriptions should include enough context for an agent to work without
  asking clarifying questions — mention relevant files, patterns, and constraints
- If items have ordering dependencies, note them in the descriptions
- Keep items focused — prefer several small items over one large item

Record the IDs of all created work items.

When all work items are created, advance:
`sol workflow advance --world={{world}} --agent=$SOL_AGENT`
