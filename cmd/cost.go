package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var (
	costWorld   string
	costAgent   string
	costCaravan string
	costSince   string
	costJSON    bool
)

var costCmd = &cobra.Command{
	Use:   "cost",
	Short: "Show token usage and cost across worlds",
	Long: `Show token usage and estimated cost.

Without flags, shows sphere-wide per-world cost totals.
With --world, shows per-agent breakdown within a world.
With --agent and --world, shows per-writ breakdown for an agent.
With --caravan, shows per-writ breakdown across worlds for a caravan.
With --since, filters by time window (relative duration or absolute date).`,
	GroupID:      groupDispatch,
	SilenceUsage: true,
	RunE:         runCost,
}

func init() {
	rootCmd.AddCommand(costCmd)
	costCmd.Flags().StringVar(&costWorld, "world", "", "world name")
	costCmd.Flags().StringVar(&costAgent, "agent", "", "show per-writ breakdown for an agent (requires --world)")
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
	if costAgent != "" && costWorld == "" {
		return fmt.Errorf("--agent requires --world")
	}
	if costAgent != "" && costCaravan != "" {
		return fmt.Errorf("--agent and --caravan cannot be used together")
	}
	if costWorld != "" && costCaravan != "" {
		return fmt.Errorf("--world and --caravan cannot be used together")
	}

	// Load pricing config.
	pricing, err := config.LoadPricing()
	if err != nil {
		return fmt.Errorf("failed to load pricing: %w", err)
	}

	switch {
	case costCaravan != "":
		return runCostCaravan(pricing, since)
	case costAgent != "":
		return runCostAgent(pricing, since)
	case costWorld != "":
		return runCostWorld(pricing, since)
	default:
		return runCostSphere(pricing, since)
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

// --- Sphere-level cost (default) ---

// sphereCostRow holds per-world cost data for sphere view.
type sphereCostRow struct {
	World        string `json:"world"`
	Agents       int    `json:"agents"`
	Writs        int    `json:"writs"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	CacheTokens  int64  `json:"cache_tokens"`
	Cost         *float64 `json:"cost,omitempty"`
	Unpriced     []string `json:"unpriced,omitempty"`
}

type sphereCostResult struct {
	Rows         []sphereCostRow `json:"worlds"`
	TotalCost    *float64        `json:"total_cost,omitempty"`
	AllUnpriced  []string        `json:"unpriced_models,omitempty"`
	HasPricing   bool            `json:"has_pricing"`
	PricingCount int             `json:"pricing_model_count"`
	Period       string          `json:"period"`
}

func runCostSphere(pricing config.PricingConfig, since *time.Time) error {
	sphereStore, err := store.OpenSphere()
	if err != nil {
		return err
	}
	defer sphereStore.Close()

	worlds, err := sphereStore.ListWorlds()
	if err != nil {
		return err
	}

	hasPricing := len(pricing) > 0
	var rows []sphereCostRow
	var totalCost float64
	allUnpriced := make(map[string]bool)

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
				agents, writs, _ = worldStore.WorldTokenMetaSince(*since)
			}
		} else {
			summaries, err = worldStore.TokensForWorld()
			if err == nil {
				agents, writs, _ = worldStore.WorldTokenMeta()
			}
		}
		worldStore.Close()
		if err != nil {
			continue
		}

		if len(summaries) == 0 {
			continue
		}

		row := sphereCostRow{
			World:  w.Name,
			Agents: agents,
			Writs:  writs,
		}

		for _, ts := range summaries {
			row.InputTokens += ts.InputTokens
			row.OutputTokens += ts.OutputTokens
			row.CacheTokens += ts.CacheReadTokens + ts.CacheCreationTokens
		}

		if hasPricing {
			cfgSummaries := storeToConfigSummaries(summaries)
			cost, unpriced := pricing.ComputeCost(cfgSummaries)
			row.Cost = &cost
			row.Unpriced = unpriced
			totalCost += cost
			for _, m := range unpriced {
				allUnpriced[m] = true
			}
		}

		rows = append(rows, row)
	}

	period := "all time"
	if since != nil {
		period = fmt.Sprintf("since %s", since.Format("2006-01-02"))
	}

	result := sphereCostResult{
		Rows:         rows,
		HasPricing:   hasPricing,
		PricingCount: len(pricing),
		Period:       period,
	}
	if hasPricing {
		result.TotalCost = &totalCost
	}
	if len(allUnpriced) > 0 {
		for m := range allUnpriced {
			result.AllUnpriced = append(result.AllUnpriced, m)
		}
		sort.Strings(result.AllUnpriced)
	}

	if costJSON {
		return printJSON(result)
	}

	renderSphereCost(result, hasPricing)
	return nil
}

func renderSphereCost(result sphereCostResult, hasPricing bool) {
	if len(result.Rows) == 0 {
		fmt.Println("No token usage data found.")
		return
	}

	if !hasPricing {
		fmt.Println("No pricing configured. Add [pricing] section to sol.toml. Showing token counts only.")
		fmt.Println()
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 3, ' ', tabwriter.AlignRight)
	if hasPricing {
		fmt.Fprintf(tw, "World\tAgents\tWrits\tInput Tokens\tOutput Tokens\tCache Tokens\tCost\t\n")
	} else {
		fmt.Fprintf(tw, "World\tAgents\tWrits\tInput Tokens\tOutput Tokens\tCache Tokens\t\n")
	}

	for _, row := range result.Rows {
		if hasPricing {
			costStr := "unpriced"
			if row.Cost != nil {
				costStr = formatDollars(*row.Cost)
			}
			fmt.Fprintf(tw, "%s\t%d\t%d\t%s\t%s\t%s\t%s\t\n",
				row.World, row.Agents, row.Writs,
				formatTokenInt(row.InputTokens),
				formatTokenInt(row.OutputTokens),
				formatTokenInt(row.CacheTokens),
				costStr)
		} else {
			fmt.Fprintf(tw, "%s\t%d\t%d\t%s\t%s\t%s\t\n",
				row.World, row.Agents, row.Writs,
				formatTokenInt(row.InputTokens),
				formatTokenInt(row.OutputTokens),
				formatTokenInt(row.CacheTokens))
		}
	}

	if hasPricing && result.TotalCost != nil {
		fmt.Fprintf(tw, "\t\t\t\t\tTotal:\t%s\t\n", formatDollars(*result.TotalCost))
	}
	tw.Flush()

	fmt.Println()
	fmt.Printf("Period: %s\n", result.Period)
	if hasPricing {
		fmt.Printf("Pricing: sol.toml (%d models configured)\n", result.PricingCount)
	}

	if len(result.AllUnpriced) > 0 {
		fmt.Printf("\n%d unpriced model(s): %s. Add to [pricing] in sol.toml.\n",
			len(result.AllUnpriced), strings.Join(result.AllUnpriced, ", "))
	}
}

// --- World-level cost (--world) ---

type worldCostRow struct {
	Agent        string   `json:"agent"`
	Writs        int      `json:"writs"`
	InputTokens  int64    `json:"input_tokens"`
	OutputTokens int64    `json:"output_tokens"`
	CacheTokens  int64    `json:"cache_tokens"`
	Cost         *float64 `json:"cost,omitempty"`
}

type worldCostResult struct {
	World        string         `json:"world"`
	Rows         []worldCostRow `json:"agents"`
	TotalCost    *float64       `json:"total_cost,omitempty"`
	AllUnpriced  []string       `json:"unpriced_models,omitempty"`
	HasPricing   bool           `json:"has_pricing"`
	Period       string         `json:"period"`
}

func runCostWorld(pricing config.PricingConfig, since *time.Time) error {
	if err := config.RequireWorld(costWorld); err != nil {
		return err
	}

	worldStore, err := store.OpenWorld(costWorld)
	if err != nil {
		return err
	}
	defer worldStore.Close()

	hasPricing := len(pricing) > 0

	var agentSummaries []store.AgentTokenSummary
	if since != nil {
		agentSummaries, err = worldStore.TokensByAgentSince(*since)
	} else {
		agentSummaries, err = worldStore.TokensByAgentForWorld()
	}
	if err != nil {
		return err
	}

	var rows []worldCostRow
	var totalCost float64
	allUnpriced := make(map[string]bool)

	for _, ats := range agentSummaries {
		row := worldCostRow{
			Agent:        ats.AgentName,
			Writs:        ats.WritCount,
			InputTokens:  ats.InputTokens,
			OutputTokens: ats.OutputTokens,
			CacheTokens:  ats.CacheReadTokens + ats.CacheCreationTokens,
		}

		if hasPricing {
			// For accurate per-model pricing, query per-model breakdown for this agent.
			var modelSummaries []store.TokenSummary
			modelSummaries, err = worldStore.AggregateTokens(ats.AgentName)
			if err != nil {
				return err
			}
			cfgSummaries := storeToConfigSummaries(modelSummaries)
			cost, unpriced := pricing.ComputeCost(cfgSummaries)
			row.Cost = &cost
			totalCost += cost
			for _, m := range unpriced {
				allUnpriced[m] = true
			}
		}

		rows = append(rows, row)
	}

	period := "all time"
	if since != nil {
		period = fmt.Sprintf("since %s", since.Format("2006-01-02"))
	}

	result := worldCostResult{
		World:      costWorld,
		Rows:       rows,
		HasPricing: hasPricing,
		Period:     period,
	}
	if hasPricing {
		result.TotalCost = &totalCost
	}
	if len(allUnpriced) > 0 {
		for m := range allUnpriced {
			result.AllUnpriced = append(result.AllUnpriced, m)
		}
		sort.Strings(result.AllUnpriced)
	}

	if costJSON {
		return printJSON(result)
	}

	renderWorldCost(result, hasPricing)
	return nil
}

func renderWorldCost(result worldCostResult, hasPricing bool) {
	if len(result.Rows) == 0 {
		fmt.Printf("No token usage data found for world %q.\n", result.World)
		return
	}

	if !hasPricing {
		fmt.Println("No pricing configured. Add [pricing] section to sol.toml. Showing token counts only.")
		fmt.Println()
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 3, ' ', tabwriter.AlignRight)
	if hasPricing {
		fmt.Fprintf(tw, "Agent\tWrits\tInput Tokens\tOutput Tokens\tCache Tokens\tCost\t\n")
	} else {
		fmt.Fprintf(tw, "Agent\tWrits\tInput Tokens\tOutput Tokens\tCache Tokens\t\n")
	}

	for _, row := range result.Rows {
		if hasPricing {
			costStr := "unpriced"
			if row.Cost != nil {
				costStr = formatDollars(*row.Cost)
			}
			fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%s\t\n",
				row.Agent, row.Writs,
				formatTokenInt(row.InputTokens),
				formatTokenInt(row.OutputTokens),
				formatTokenInt(row.CacheTokens),
				costStr)
		} else {
			fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t\n",
				row.Agent, row.Writs,
				formatTokenInt(row.InputTokens),
				formatTokenInt(row.OutputTokens),
				formatTokenInt(row.CacheTokens))
		}
	}

	if hasPricing && result.TotalCost != nil {
		fmt.Fprintf(tw, "\t\t\t\tTotal:\t%s\t\n", formatDollars(*result.TotalCost))
	}
	tw.Flush()

	if len(result.AllUnpriced) > 0 {
		fmt.Printf("\n%d unpriced model(s): %s. Add to [pricing] in sol.toml.\n",
			len(result.AllUnpriced), strings.Join(result.AllUnpriced, ", "))
	}
}

// --- Agent-level cost (--agent --world) ---

type agentCostRow struct {
	WritID       string   `json:"writ_id"`
	Kind         string   `json:"kind"`
	Status       string   `json:"status"`
	InputTokens  int64    `json:"input_tokens"`
	OutputTokens int64    `json:"output_tokens"`
	CacheTokens  int64    `json:"cache_tokens"`
	Cost         *float64 `json:"cost,omitempty"`
}

type agentCostResult struct {
	World       string         `json:"world"`
	Agent       string         `json:"agent"`
	Rows        []agentCostRow `json:"writs"`
	TotalCost   *float64       `json:"total_cost,omitempty"`
	AllUnpriced []string       `json:"unpriced_models,omitempty"`
	HasPricing  bool           `json:"has_pricing"`
	Period      string         `json:"period"`
}

func runCostAgent(pricing config.PricingConfig, since *time.Time) error {
	if err := config.RequireWorld(costWorld); err != nil {
		return err
	}

	worldStore, err := store.OpenWorld(costWorld)
	if err != nil {
		return err
	}
	defer worldStore.Close()

	hasPricing := len(pricing) > 0

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

	var rows []agentCostRow
	var totalCost float64
	allUnpriced := make(map[string]bool)

	for _, writID := range writIDs {
		summaries := writTokens[writID]
		row := agentCostRow{WritID: writID}

		// Look up writ metadata.
		if writID != "" {
			writ, wErr := worldStore.GetWrit(writID)
			if wErr == nil {
				row.Kind = writ.Kind
				row.Status = writ.Status
			}
		}

		for _, ts := range summaries {
			row.InputTokens += ts.InputTokens
			row.OutputTokens += ts.OutputTokens
			row.CacheTokens += ts.CacheReadTokens + ts.CacheCreationTokens
		}

		if hasPricing {
			cfgSummaries := storeToConfigSummaries(summaries)
			cost, unpriced := pricing.ComputeCost(cfgSummaries)
			row.Cost = &cost
			totalCost += cost
			for _, m := range unpriced {
				allUnpriced[m] = true
			}
		}

		rows = append(rows, row)
	}

	period := "all time"
	if since != nil {
		period = fmt.Sprintf("since %s", since.Format("2006-01-02"))
	}

	result := agentCostResult{
		World:      costWorld,
		Agent:      costAgent,
		Rows:       rows,
		HasPricing: hasPricing,
		Period:     period,
	}
	if hasPricing {
		result.TotalCost = &totalCost
	}
	if len(allUnpriced) > 0 {
		for m := range allUnpriced {
			result.AllUnpriced = append(result.AllUnpriced, m)
		}
		sort.Strings(result.AllUnpriced)
	}

	if costJSON {
		return printJSON(result)
	}

	renderAgentCost(result, hasPricing)
	return nil
}

func renderAgentCost(result agentCostResult, hasPricing bool) {
	if len(result.Rows) == 0 {
		fmt.Printf("No token usage data found for agent %q in world %q.\n", result.Agent, result.World)
		return
	}

	if !hasPricing {
		fmt.Println("No pricing configured. Add [pricing] section to sol.toml. Showing token counts only.")
		fmt.Println()
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 3, ' ', tabwriter.AlignRight)
	if hasPricing {
		fmt.Fprintf(tw, "Writ\tKind\tStatus\tInput\tOutput\tCache\tCost\t\n")
	} else {
		fmt.Fprintf(tw, "Writ\tKind\tStatus\tInput\tOutput\tCache\t\n")
	}

	for _, row := range result.Rows {
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

		if hasPricing {
			costStr := "unpriced"
			if row.Cost != nil {
				costStr = formatDollars(*row.Cost)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t\n",
				wid, kind, status,
				formatTokenInt(row.InputTokens),
				formatTokenInt(row.OutputTokens),
				formatTokenInt(row.CacheTokens),
				costStr)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t\n",
				wid, kind, status,
				formatTokenInt(row.InputTokens),
				formatTokenInt(row.OutputTokens),
				formatTokenInt(row.CacheTokens))
		}
	}

	if hasPricing && result.TotalCost != nil {
		fmt.Fprintf(tw, "\t\t\t\tTotal:\t%s\t\n", formatDollars(*result.TotalCost))
	}
	tw.Flush()

	if len(result.AllUnpriced) > 0 {
		fmt.Printf("\n%d unpriced model(s): %s. Add to [pricing] in sol.toml.\n",
			len(result.AllUnpriced), strings.Join(result.AllUnpriced, ", "))
	}
}

// --- Caravan-level cost (--caravan) ---

type caravanCostRow struct {
	WritID       string   `json:"writ_id"`
	World        string   `json:"world"`
	Phase        int      `json:"phase"`
	Kind         string   `json:"kind"`
	Status       string   `json:"status"`
	InputTokens  int64    `json:"input_tokens"`
	OutputTokens int64    `json:"output_tokens"`
	CacheTokens  int64    `json:"cache_tokens"`
	Cost         *float64 `json:"cost,omitempty"`
}

type caravanCostResult struct {
	CaravanID   string           `json:"caravan_id"`
	CaravanName string           `json:"caravan_name"`
	Rows        []caravanCostRow `json:"writs"`
	TotalCost   *float64         `json:"total_cost,omitempty"`
	AllUnpriced []string         `json:"unpriced_models,omitempty"`
	HasPricing  bool             `json:"has_pricing"`
	Period      string           `json:"period"`
}

func runCostCaravan(pricing config.PricingConfig, since *time.Time) error {
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

	hasPricing := len(pricing) > 0
	var rows []caravanCostRow
	var totalCost float64
	allUnpriced := make(map[string]bool)

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

			row := caravanCostRow{
				WritID: item.WritID,
				World:  world,
				Phase:  item.Phase,
			}

			// Look up writ metadata.
			writ, wErr := worldStore.GetWrit(item.WritID)
			if wErr == nil {
				row.Kind = writ.Kind
				row.Status = writ.Status
			}

			for _, ts := range summaries {
				row.InputTokens += ts.InputTokens
				row.OutputTokens += ts.OutputTokens
				row.CacheTokens += ts.CacheReadTokens + ts.CacheCreationTokens
			}

			if hasPricing {
				cfgSummaries := storeToConfigSummaries(summaries)
				cost, unpriced := pricing.ComputeCost(cfgSummaries)
				row.Cost = &cost
				totalCost += cost
				for _, m := range unpriced {
					allUnpriced[m] = true
				}
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

	result := caravanCostResult{
		CaravanID:   caravan.ID,
		CaravanName: caravan.Name,
		Rows:        rows,
		HasPricing:  hasPricing,
		Period:      period,
	}
	if hasPricing {
		result.TotalCost = &totalCost
	}
	if len(allUnpriced) > 0 {
		for m := range allUnpriced {
			result.AllUnpriced = append(result.AllUnpriced, m)
		}
		sort.Strings(result.AllUnpriced)
	}

	if costJSON {
		return printJSON(result)
	}

	renderCaravanCost(result, hasPricing)
	return nil
}

func renderCaravanCost(result caravanCostResult, hasPricing bool) {
	fmt.Printf("Caravan: %s (%s)\n\n", result.CaravanName, result.CaravanID)

	if len(result.Rows) == 0 {
		fmt.Println("No token usage data found.")
		return
	}

	if !hasPricing {
		fmt.Println("No pricing configured. Add [pricing] section to sol.toml. Showing token counts only.")
		fmt.Println()
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 3, ' ', tabwriter.AlignRight)
	if hasPricing {
		fmt.Fprintf(tw, "Writ\tWorld\tPhase\tKind\tStatus\tInput\tOutput\tCache\tCost\t\n")
	} else {
		fmt.Fprintf(tw, "Writ\tWorld\tPhase\tKind\tStatus\tInput\tOutput\tCache\t\n")
	}

	for _, row := range result.Rows {
		kind := row.Kind
		if kind == "" {
			kind = "-"
		}
		status := row.Status
		if status == "" {
			status = "-"
		}

		if hasPricing {
			costStr := "unpriced"
			if row.Cost != nil {
				costStr = formatDollars(*row.Cost)
			}
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\t%s\t\n",
				row.WritID, row.World, row.Phase, kind, status,
				formatTokenInt(row.InputTokens),
				formatTokenInt(row.OutputTokens),
				formatTokenInt(row.CacheTokens),
				costStr)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\t\n",
				row.WritID, row.World, row.Phase, kind, status,
				formatTokenInt(row.InputTokens),
				formatTokenInt(row.OutputTokens),
				formatTokenInt(row.CacheTokens))
		}
	}

	if hasPricing && result.TotalCost != nil {
		fmt.Fprintf(tw, "\t\t\t\t\t\tTotal:\t%s\t\n", formatDollars(*result.TotalCost))
	}
	tw.Flush()

	if len(result.AllUnpriced) > 0 {
		fmt.Printf("\n%d unpriced model(s): %s. Add to [pricing] in sol.toml.\n",
			len(result.AllUnpriced), strings.Join(result.AllUnpriced, ", "))
	}
}

// --- Helpers ---

// resolveCaravan finds a caravan by ID or name.
func resolveCaravan(sphereStore *store.Store, idOrName string) (*store.Caravan, error) {
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

// storeToConfigSummaries converts store.TokenSummary to config.TokenSummary
// to avoid import cycles between the two packages.
func storeToConfigSummaries(summaries []store.TokenSummary) []config.TokenSummary {
	out := make([]config.TokenSummary, len(summaries))
	for i, ts := range summaries {
		out[i] = config.TokenSummary{
			Model:               ts.Model,
			InputTokens:         ts.InputTokens,
			OutputTokens:        ts.OutputTokens,
			CacheReadTokens:     ts.CacheReadTokens,
			CacheCreationTokens: ts.CacheCreationTokens,
		}
	}
	return out
}

// formatDollars formats a dollar amount for display.
func formatDollars(amount float64) string {
	if amount == 0 {
		return "$0.00"
	}
	return fmt.Sprintf("$%.2f", amount)
}
