# Chart the Fix Caravan

Create the fix writs and commission a drydocked caravan.

For each fix designed in the previous step:

1. Create a writ:
   ```
   sol writ create --title="<fix title>" \
     --description="<what to fix, where, why, how>" \
     --world=$SOL_WORLD
   ```

2. Set up dependencies between items where needed:
   ```
   sol writ dep add <from-id> <to-id>
   ```

3. Commission a drydocked caravan grouping all fix items:
   ```
   sol caravan create "<descriptive name>"
   sol caravan add <caravan-id> <id1> <id2> ...
   sol caravan commission <caravan-id>
   ```

4. Report the caravan ID so the operator can review and launch it:
   ```
   sol escalate "Deep scan complete for {{issue}}. Fix caravan: <caravan_id>"
   ```

When the caravan is created and reported, advance:
`sol workflow advance --world=$SOL_WORLD --agent=$SOL_AGENT`
