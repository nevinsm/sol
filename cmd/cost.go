package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	clicost "github.com/nevinsm/sol/internal/cliapi/cost"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var (
	costWorld   string
	costAgent   string
	costWrit    string
	costCaravan string
	costSince   string
	costJSON    bool
)

var costCmd = &cobra.Command{
	Use:   "cost",
	Short: "Show token usage and cost across worlds",
	Long: `Show token usage and estimated cost.

Without flags, shows sphere-wide per-world cost totals (no world detection
is applied — sphere-wide is the explicit default).
With --world, shows per-agent breakdown within a world.
With --agent, shows per-writ breakdown for an agent in a world.
With --writ, shows per-model breakdown for a specific writ.
With --caravan, shows per-writ breakdown across worlds for a caravan.
With --since, filters by time window (relative duration or absolute date).

For the --world, --agent, and --writ branches, the world is resolved using
the standard precedence: explicit --world flag > SOL_WORLD env var > cwd
detection (when run from inside a world directory under $SOL_HOME).`,
	GroupID:      groupDispatch,
	SilenceUsage: true,
	RunE:         runCost,
}

func init() {
	rootCmd.AddCommand(costCmd)
	costCmd.Flags().StringVar(&costWorld, "world", "", "world name")
	costCmd.Flags().StringVar(&costAgent, "agent", "", "show per-writ breakdown for an agent (requires --world)")
	costCmd.Flags().StringVar(&costWrit, "writ", "", "show per-model breakdown for a writ (requires --world)")
	costCmd.Flags().StringVar(&costCaravan, "caravan", "", "show per-writ breakdown for a caravan (ID or name)")
	costCmd.Flags().StringVar(&costSince, "since", "", "time window: relative duration (24h) or absolute date (2006-01-02)")
	costCmd.Flags().BoolVar(&costJSON, "json", false, "output as JSON")
}

func runCost(cmd *cobra.Command, args []string) error {
	// Parse --since if provided.
	var since *time.Time
	if costSince != "" {
		t, err := parseSinceFlag(costSince)
		if err != nil {
			return err
		}
		since = &t
	}

	// Validate flag combinations.
	if costAgent != "" && costCaravan != "" {
		return fmt.Errorf("--agent and --caravan cannot be used together")
	}
	if costWrit != "" && costAgent != "" {
		return fmt.Errorf("--writ and --agent cannot be used together")
	}
	if costWrit != "" && costCaravan != "" {
		return fmt.Errorf("--writ and --caravan cannot be used together")
	}
	if costWorld != "" && costCaravan != "" {
		return fmt.Errorf("--world and --caravan cannot be used together")
	}

	// Resolve --world for branches that operate on a single world. The
	// sphere-wide default (no flags) and --caravan branches are explicitly
	// not world-scoped — preserve those by skipping resolution.
	//
	// ResolveWorld honors the standard precedence:
	//   explicit --world flag > SOL_WORLD env > cwd detection.
	// This lets `sol cost --agent=foo` work without --world when run from
	// inside a worktree under $SOL_HOME/{world}/.
	if costCaravan == "" && (costAgent != "" || costWrit != "" || costWorld != "") {
		resolved, err := config.ResolveWorld(costWorld)
		if err != nil {
			return err
		}
		costWorld = resolved
	}

	switch {
	case costCaravan != "":
		return runCostCaravan(since)
	case costWrit != "":
		return runCostWrit(since)
	case costAgent != "":
		return runCostAgent(since)
	case costWorld != "":
		return runCostWorld(since)
	default:
		return runCostSphere(since)
	}
}

// parseSinceFlag parses a --since flag value as either a relative duration or
// absolute date. Supported formats:
//   - Relative: "24h", "30m", "7d" (d is expanded to 24h)
//   - Absolute: "2006-01-02" (date only) or RFC3339
func parseSinceFlag(s string) (time.Time, error) {
	// Try Go duration first (e.g., "24h", "30m").
	dur, err := time.ParseDuration(s)
	if err == nil {
		return time.Now().Add(-dur), nil
	}

	// Try "Nd" shorthand for days.
	if strings.HasSuffix(s, "d") {
		dayStr := strings.TrimSuffix(s, "d")
		dur, err = time.ParseDuration(dayStr + "h")
		if err == nil {
			return time.Now().Add(-dur * 24), nil
		}
	}

	// Try date-only format "2006-01-02".
	t, err := time.Parse("2006-01-02", s)
	if err == nil {
		return t.UTC(), nil
	}

	// Try RFC3339.
	t, err = time.Parse(time.RFC3339, s)
	if err == nil {
		return t.UTC(), nil
	}

	return time.Time{}, fmt.Errorf("invalid --since %q: expected duration (24h, 7d), date (2006-01-02), or RFC3339", s)
}

// sumCostUSD sums CostUSD from token summaries. Returns nil if any summary
// has a nil CostUSD (partial totals are misleading).
func sumCostUSD(summaries []store.TokenSummary) *float64 {
	var total float64
	for _, ts := range summaries {
		if ts.CostUSD == nil {
			return nil
		}
		total += *ts.CostUSD
	}
	return &total
}

// --- Sphere-level cost (default) ---

func runCostSphere(since *time.Time) error {
	sphereStore, err := store.OpenSphere()
	if err != nil {
		return err
	}
	defer sphereStore.Close()

	worlds, err := sphereStore.ListWorlds()
	if err != nil {
		return err
	}

	var rows []clicost.WorldCost
	var totalCost float64
	anyNilCost := false

	for _, w := range worlds {
		worldStore, err := store.OpenWorld(w.Name)
		if err != nil {
			// Skip worlds that can't be opened (e.g., missing DB).
			continue
		}

		var summaries []store.TokenSummary
		var agents, writs int
		if since != nil {
			summaries, err = worldStore.TokensSince(*since)
			if err == nil {
				var metaErr error
				agents, writs, metaErr = worldStore.WorldTokenMetaSince(*since)
				if metaErr != nil {
					fmt.Fprintf(os.Stderr, "warning: world token meta since: %v\n", metaErr)
				}
			}
		} else {
			summaries, err = worldStore.TokensForWorld()
			if err == nil {
				var metaErr error
				agents, writs, metaErr = worldStore.WorldTokenMeta()
				if metaErr != nil {
					fmt.Fprintf(os.Stderr, "warning: world token meta: %v\n", metaErr)
				}
			}
		}
		worldStore.Close()
		if err != nil {
			continue
		}

		if len(summaries) == 0 {
			continue
		}

		row := clicost.WorldCost{
			World:  w.Name,
			Agents: agents,
			Writs:  writs,
		}

		for _, ts := range summaries {
			row.InputTokens += ts.InputTokens
			row.OutputTokens += ts.OutputTokens
			row.CacheTokens += ts.CacheReadTokens + ts.CacheCreationTokens
		}

		rowCost := sumCostUSD(summaries)
		row.Cost = rowCost
		if rowCost == nil {
			anyNilCost = true
		} else {
			totalCost += *rowCost
		}

		rows = append(rows, row)
	}

	period := "all time"
	if since != nil {
		period = fmt.Sprintf("since %s", since.Format("2006-01-02"))
	}

	result := clicost.CostSummary{
		Worlds: rows,
		Period: period,
	}
	if !anyNilCost {
		result.TotalCost = &totalCost
	}

	if costJSON {
		return printJSON(result)
	}

	renderSphereCost(result)
	return nil
}

func renderSphereCost(result clicost.CostSummary) {
	if len(result.Worlds) == 0 {
		fmt.Println("No token usage data found.")
		return
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 3, ' ', tabwriter.AlignRight)
	fmt.Fprintf(tw, "World\tAgents\tWrits\tInput Tokens\tOutput Tokens\tCache Tokens\tCost\t\n")

	for _, row := range result.Worlds {
		costStr := "N/A"
		if row.Cost != nil {
			costStr = formatDollars(*row.Cost)
		}
		fmt.Fprintf(tw, "%s\t%d\t%d\t%s\t%s\t%s\t%s\t\n",
			row.World, row.Agents, row.Writs,
			formatTokenInt(row.InputTokens),
			formatTokenInt(row.OutputTokens),
			formatTokenInt(row.CacheTokens),
			costStr)
	}

	totalStr := "N/A"
	if result.TotalCost != nil {
		totalStr = formatDollars(*result.TotalCost)
	}
	fmt.Fprintf(tw, "\t\t\t\t\tTotal:\t%s\t\n", totalStr)
	tw.Flush()

	fmt.Println()
	fmt.Printf("Period: %s\n", result.Period)
}

// --- World-level cost (--world) ---

func runCostWorld(since *time.Time) error {
	if err := config.RequireWorld(costWorld); err != nil {
		return err
	}

	worldStore, err := store.OpenWorld(costWorld)
	if err != nil {
		return err
	}
	defer worldStore.Close()

	var agentSummaries []store.AgentTokenSummary
	if since != nil {
		agentSummaries, err = worldStore.TokensByAgentSince(*since)
	} else {
		agentSummaries, err = worldStore.TokensByAgentForWorld()
	}
	if err != nil {
		return err
	}

	var rows []clicost.AgentCost
	var totalCost float64
	anyNilCost := false

	for _, ats := range agentSummaries {
		row := clicost.AgentCost{
			Agent:        ats.AgentName,
			Writs:        ats.WritCount,
			InputTokens:  ats.InputTokens,
			OutputTokens: ats.OutputTokens,
			CacheTokens:  ats.CacheReadTokens + ats.CacheCreationTokens,
			Cost:         ats.CostUSD,
		}

		if ats.CostUSD == nil {
			anyNilCost = true
		} else {
			totalCost += *ats.CostUSD
		}

		rows = append(rows, row)
	}

	period := "all time"
	if since != nil {
		period = fmt.Sprintf("since %s", since.Format("2006-01-02"))
	}

	result := clicost.WorldCostResponse{
		World:  costWorld,
		Agents: rows,
		Period: period,
	}
	if !anyNilCost {
		result.TotalCost = &totalCost
	}

	if costJSON {
		return printJSON(result)
	}

	renderWorldCost(result)
	return nil
}

func renderWorldCost(result clicost.WorldCostResponse) {
	if len(result.Agents) == 0 {
		fmt.Printf("No token usage data found for world %q.\n", result.World)
		return
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 3, ' ', tabwriter.AlignRight)
	fmt.Fprintf(tw, "Agent\tWrits\tInput Tokens\tOutput Tokens\tCache Tokens\tCost\t\n")

	for _, row := range result.Agents {
		costStr := "N/A"
		if row.Cost != nil {
			costStr = formatDollars(*row.Cost)
		}
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%s\t\n",
			row.Agent, row.Writs,
			formatTokenInt(row.InputTokens),
			formatTokenInt(row.OutputTokens),
			formatTokenInt(row.CacheTokens),
			costStr)
	}

	totalStr := "N/A"
	if result.TotalCost != nil {
		totalStr = formatDollars(*result.TotalCost)
	}
	fmt.Fprintf(tw, "\t\t\t\tTotal:\t%s\t\n", totalStr)
	tw.Flush()
}

// --- Agent-level cost (--agent --world) ---

func runCostAgent(since *time.Time) error {
	if err := config.RequireWorld(costWorld); err != nil {
		return err
	}

	worldStore, err := store.OpenWorld(costWorld)
	if err != nil {
		return err
	}
	defer worldStore.Close()

	var writTokens map[string][]store.TokenSummary
	if since != nil {
		writTokens, err = worldStore.TokensByWritForAgentSince(costAgent, *since)
	} else {
		writTokens, err = worldStore.TokensByWritForAgent(costAgent)
	}
	if err != nil {
		return err
	}

	// Sort writ IDs for stable output.
	writIDs := make([]string, 0, len(writTokens))
	for wid := range writTokens {
		writIDs = append(writIDs, wid)
	}
	sort.Strings(writIDs)

	var rows []clicost.WritCost
	var totalCost float64
	anyNilCost := false

	for _, writID := range writIDs {
		summaries := writTokens[writID]
		row := clicost.WritCost{WritID: writID}

		// Look up writ metadata.
		if writID != "" {
			writ, wErr := worldStore.GetWrit(writID)
			if wErr == nil {
				row.Kind = writ.Kind
				row.Status = string(writ.Status)
			}
		}

		for _, ts := range summaries {
			row.InputTokens += ts.InputTokens
			row.OutputTokens += ts.OutputTokens
			row.CacheTokens += ts.CacheReadTokens + ts.CacheCreationTokens
		}

		rowCost := sumCostUSD(summaries)
		row.Cost = rowCost
		if rowCost == nil {
			anyNilCost = true
		} else {
			totalCost += *rowCost
		}

		rows = append(rows, row)
	}

	period := "all time"
	if since != nil {
		period = fmt.Sprintf("since %s", since.Format("2006-01-02"))
	}

	result := clicost.AgentCostResponse{
		World:  costWorld,
		Agent:  costAgent,
		Writs:  rows,
		Period: period,
	}
	if !anyNilCost {
		result.TotalCost = &totalCost
	}

	if costJSON {
		return printJSON(result)
	}

	renderAgentCost(result)
	return nil
}

func renderAgentCost(result clicost.AgentCostResponse) {
	if len(result.Writs) == 0 {
		fmt.Printf("No token usage data found for agent %q in world %q.\n", result.Agent, result.World)
		return
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 3, ' ', tabwriter.AlignRight)
	fmt.Fprintf(tw, "Writ\tKind\tStatus\tInput\tOutput\tCache\tCost\t\n")

	for _, row := range result.Writs {
		wid := row.WritID
		if wid == "" {
			wid = "(no writ)"
		}
		kind := row.Kind
		if kind == "" {
			kind = "-"
		}
		status := row.Status
		if status == "" {
			status = "-"
		}

		costStr := "N/A"
		if row.Cost != nil {
			costStr = formatDollars(*row.Cost)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t\n",
			wid, kind, status,
			formatTokenInt(row.InputTokens),
			formatTokenInt(row.OutputTokens),
			formatTokenInt(row.CacheTokens),
			costStr)
	}

	totalStr := "N/A"
	if result.TotalCost != nil {
		totalStr = formatDollars(*result.TotalCost)
	}
	fmt.Fprintf(tw, "\t\t\t\tTotal:\t%s\t\n", totalStr)
	tw.Flush()
}

// --- Writ-level cost (--writ --world) ---

func runCostWrit(since *time.Time) error {
	if err := config.RequireWorld(costWorld); err != nil {
		return err
	}

	worldStore, err := store.OpenWorld(costWorld)
	if err != nil {
		return err
	}
	defer worldStore.Close()

	// Look up writ metadata.
	writ, err := worldStore.GetWrit(costWrit)
	if err != nil {
		return fmt.Errorf("writ %q not found in world %q", costWrit, costWorld)
	}

	var summaries []store.TokenSummary
	if since != nil {
		summaries, err = worldStore.TokensForWritSince(costWrit, *since)
	} else {
		summaries, err = worldStore.TokensForWrit(costWrit)
	}
	if err != nil {
		return err
	}

	var rows []clicost.ModelCost
	var totalCost float64
	anyNilCost := false

	for _, ts := range summaries {
		row := clicost.ModelCost{
			Model:               ts.Model,
			InputTokens:         ts.InputTokens,
			OutputTokens:        ts.OutputTokens,
			CacheReadTokens:     ts.CacheReadTokens,
			CacheCreationTokens: ts.CacheCreationTokens,
			Cost:                ts.CostUSD,
		}

		if ts.CostUSD == nil {
			anyNilCost = true
		} else {
			totalCost += *ts.CostUSD
		}

		rows = append(rows, row)
	}

	period := "all time"
	if since != nil {
		period = fmt.Sprintf("since %s", since.Format("2006-01-02"))
	}

	result := clicost.WritCostResponse{
		WritID: costWrit,
		Title:  writ.Title,
		Kind:   writ.Kind,
		Status: string(writ.Status),
		Models: rows,
		Period: period,
	}
	if !anyNilCost {
		result.TotalCost = &totalCost
	}

	if costJSON {
		return printJSON(result)
	}

	renderWritCost(result)
	return nil
}

func renderWritCost(result clicost.WritCostResponse) {
	// Print writ header.
	header := fmt.Sprintf("Writ: %s", result.WritID)
	if result.Title != "" {
		header += fmt.Sprintf(" — %s", result.Title)
	}
	meta := []string{}
	if result.Kind != "" {
		meta = append(meta, result.Kind)
	}
	if result.Status != "" {
		meta = append(meta, result.Status)
	}
	if len(meta) > 0 {
		header += fmt.Sprintf(" (%s)", strings.Join(meta, ", "))
	}
	fmt.Println(header)
	fmt.Println()

	if len(result.Models) == 0 {
		fmt.Println("No token usage data found.")
		return
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 3, ' ', tabwriter.AlignRight)
	fmt.Fprintf(tw, "Model\tInput\tOutput\tCache Read\tCache Create\tCost\t\n")

	var totalInput, totalOutput, totalCacheRead, totalCacheCreate int64
	for _, row := range result.Models {
		totalInput += row.InputTokens
		totalOutput += row.OutputTokens
		totalCacheRead += row.CacheReadTokens
		totalCacheCreate += row.CacheCreationTokens

		costStr := "N/A"
		if row.Cost != nil {
			costStr = formatDollars(*row.Cost)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t\n",
			row.Model,
			formatTokenInt(row.InputTokens),
			formatTokenInt(row.OutputTokens),
			formatTokenInt(row.CacheReadTokens),
			formatTokenInt(row.CacheCreationTokens),
			costStr)
	}

	// Totals row (only when more than one model).
	if len(result.Models) > 1 {
		fmt.Fprintf(tw, "\t-------\t------\t------\t------\t------\t\n")
		totalStr := "N/A"
		if result.TotalCost != nil {
			totalStr = formatDollars(*result.TotalCost)
		}
		fmt.Fprintf(tw, "Total\t%s\t%s\t%s\t%s\t%s\t\n",
			formatTokenInt(totalInput),
			formatTokenInt(totalOutput),
			formatTokenInt(totalCacheRead),
			formatTokenInt(totalCacheCreate),
			totalStr)
	}

	tw.Flush()
}

// --- Caravan-level cost (--caravan) ---

func runCostCaravan(since *time.Time) error {
	sphereStore, err := store.OpenSphere()
	if err != nil {
		return err
	}
	defer sphereStore.Close()

	// Resolve caravan by ID or name.
	caravan, err := resolveCaravan(sphereStore, costCaravan)
	if err != nil {
		return err
	}

	items, err := sphereStore.ListCaravanItems(caravan.ID)
	if err != nil {
		return err
	}

	var rows []clicost.CaravanWritCost
	var totalCost float64
	anyNilCost := false

	// Group items by world to minimize store opens.
	byWorld := make(map[string][]store.CaravanItem)
	for _, item := range items {
		byWorld[item.World] = append(byWorld[item.World], item)
	}

	for world, worldItems := range byWorld {
		worldStore, err := store.OpenWorld(world)
		if err != nil {
			continue
		}

		for _, item := range worldItems {
			var summaries []store.TokenSummary
			if since != nil {
				summaries, err = worldStore.TokensForWritSince(item.WritID, *since)
			} else {
				summaries, err = worldStore.TokensForWrit(item.WritID)
			}
			if err != nil {
				continue
			}

			row := clicost.CaravanWritCost{
				WritID: item.WritID,
				World:  world,
				Phase:  item.Phase,
			}

			// Look up writ metadata.
			writ, wErr := worldStore.GetWrit(item.WritID)
			if wErr == nil {
				row.Kind = writ.Kind
				row.Status = string(writ.Status)
			}

			for _, ts := range summaries {
				row.InputTokens += ts.InputTokens
				row.OutputTokens += ts.OutputTokens
				row.CacheTokens += ts.CacheReadTokens + ts.CacheCreationTokens
			}

			rowCost := sumCostUSD(summaries)
			row.Cost = rowCost
			if rowCost == nil {
				anyNilCost = true
			} else {
				totalCost += *rowCost
			}

			rows = append(rows, row)
		}
		worldStore.Close()
	}

	// Sort by phase then writ ID.
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Phase != rows[j].Phase {
			return rows[i].Phase < rows[j].Phase
		}
		return rows[i].WritID < rows[j].WritID
	})

	period := "all time"
	if since != nil {
		period = fmt.Sprintf("since %s", since.Format("2006-01-02"))
	}

	result := clicost.CaravanCostResponse{
		CaravanID:   caravan.ID,
		CaravanName: caravan.Name,
		Writs:       rows,
		Period:      period,
	}
	if !anyNilCost {
		result.TotalCost = &totalCost
	}

	if costJSON {
		return printJSON(result)
	}

	renderCaravanCost(result)
	return nil
}

func renderCaravanCost(result clicost.CaravanCostResponse) {
	fmt.Printf("Caravan: %s (%s)\n\n", result.CaravanName, result.CaravanID)

	if len(result.Writs) == 0 {
		fmt.Println("No token usage data found.")
		return
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 3, ' ', tabwriter.AlignRight)
	fmt.Fprintf(tw, "Writ\tWorld\tPhase\tKind\tStatus\tInput\tOutput\tCache\tCost\t\n")

	for _, row := range result.Writs {
		kind := row.Kind
		if kind == "" {
			kind = "-"
		}
		status := row.Status
		if status == "" {
			status = "-"
		}

		costStr := "N/A"
		if row.Cost != nil {
			costStr = formatDollars(*row.Cost)
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\t%s\t\n",
			row.WritID, row.World, row.Phase, kind, status,
			formatTokenInt(row.InputTokens),
			formatTokenInt(row.OutputTokens),
			formatTokenInt(row.CacheTokens),
			costStr)
	}

	totalStr := "N/A"
	if result.TotalCost != nil {
		totalStr = formatDollars(*result.TotalCost)
	}
	fmt.Fprintf(tw, "\t\t\t\t\t\tTotal:\t%s\t\n", totalStr)
	tw.Flush()
}

// --- Helpers ---

// resolveCaravan finds a caravan by ID or name.
func resolveCaravan(sphereStore *store.SphereStore, idOrName string) (*store.Caravan, error) {
	// Try by ID first.
	c, err := sphereStore.GetCaravan(idOrName)
	if err == nil {
		return c, nil
	}

	// Try by name — search all caravans.
	caravans, err := sphereStore.ListCaravans("")
	if err != nil {
		return nil, fmt.Errorf("failed to list caravans: %w", err)
	}
	for i := range caravans {
		if caravans[i].Name == idOrName {
			return &caravans[i], nil
		}
	}

	return nil, fmt.Errorf("caravan %q not found", idOrName)
}

// formatDollars formats a dollar amount for display.
func formatDollars(amount float64) string {
	if amount == 0 {
		return "$0.00"
	}
	return fmt.Sprintf("$%.2f", amount)
}
