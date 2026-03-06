# Chart the Fix Caravan

Create the fix work items and commission a drydocked caravan.

For each fix designed in the previous step:

1. Create a work item:
   ```
   sol store create --title="<fix title>" \
     --description="<what to fix, where, why, how>" \
     --world=$SOL_WORLD
   ```

2. Set up dependencies between items where needed:
   ```
   sol store dep add <item_id> --needs <dependency_id>
   ```

3. Commission a drydocked caravan grouping all fix items:
   ```
   sol caravan commission --name "<descriptive name>" \
     --items <id1>,<id2>,... --drydock
   ```

4. Report the caravan ID so the operator can review and launch it:
   ```
   sol escalate "Deep scan complete for {{issue}}. Fix caravan: <caravan_id>"
   ```

When the caravan is created and reported, advance:
`sol workflow advance --world=$SOL_WORLD --agent=$SOL_AGENT`
