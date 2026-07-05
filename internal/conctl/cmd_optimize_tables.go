package conctl

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/pkg/optimize"
)

type lineageNode struct {
	id       string
	gen      int
	score    float64
	feasible bool
}

func optimizeRunsTable(w io.Writer, data interface{}, format string) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return
	}
	runs, ok := m["runs"].([]interface{})
	if !ok {
		return
	}

	headers := []string{"ID", "WORKFLOW", "BENCHMARK", "STATUS", "GEN", "BEST", "BUDGET", "OWNER", "HEARTBEAT"}
	rows := make([][]string, 0, len(runs))
	now := time.Now().UTC()
	for _, raw := range runs {
		run, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		best := stringVal(run, "best_organism_id")
		if best == "" {
			best = "-"
		} else {
			best = truncate(best, 12)
		}
		owner := "-"
		if host := stringVal(run, "owner_hostname"); host != "" {
			pid := int(floatVal(run, "owner_pid"))
			if pid > 0 {
				owner = fmt.Sprintf("%s:%d", host, pid)
			} else {
				owner = host
			}
		}
		heartbeat := heartbeatAgeString(now, stringVal(run, "last_heartbeat_at"))
		rows = append(rows, []string{
			stringVal(run, "id"),
			stringVal(run, "workflow_id"),
			stringVal(run, "benchmark"),
			stringVal(run, "status"),
			fmt.Sprintf("%.0f", floatVal(run, "generation")),
			best,
			fmt.Sprintf("%s / %s", formatCostVal(floatVal(run, "spent_usd")), formatCostVal(floatVal(run, "total_budget_usd"))),
			owner,
			heartbeat,
		})
	}
	printTableAuto(w, headers, rows, format)
}

func optimizeRunStatusTable(w io.Writer, data interface{}, format string) {
	run, ok := data.(map[string]interface{})
	if !ok {
		return
	}

	fmt.Fprintf(w, "Run:        %s\n", stringVal(run, "id"))
	fmt.Fprintf(w, "Workflow:   %s (version %.0f)\n", stringVal(run, "workflow_id"), floatVal(run, "workflow_version"))
	fmt.Fprintf(w, "Benchmark:  %s / %s\n", stringVal(run, "benchmark"), stringVal(run, "split"))
	fmt.Fprintf(w, "Status:     %s\n", stringVal(run, "status"))
	fmt.Fprintf(w, "Generation: %.0f  Population: %.0f  Organisms: %.0f\n",
		floatVal(run, "generation"),
		floatVal(run, "population_size"),
		floatVal(run, "total_organisms"),
	)
	fmt.Fprintf(w, "Budget:     %s / %s\n",
		formatCostVal(floatVal(run, "spent_usd")),
		formatCostVal(floatVal(run, "total_budget_usd")),
	)
	fmt.Fprintf(w, "Model:      %s\n", stringVal(run, "claude_model"))
	fmt.Fprintf(w, "Mutator:    %s", optimize.NormalizeMutatorMode(coalesceVal(stringVal(run, "mutator_mode"), optimize.MutatorModeAuto)))
	if seed, ok := run["rng_seed"]; ok && seed != nil {
		fmt.Fprintf(w, "  RNG seed=%v", seed)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Fanout:     children/parent=%.0f  max/gen=%.0f  adaptive=%t\n",
		floatVal(run, "children_per_parent"),
		floatVal(run, "max_children_per_generation"),
		boolVal(run, "adaptive_fanout"),
	)
	fmt.Fprintln(w, "Failures:   flagged items excluded by default (use --include-flagged-failures to include)")
	if spec, ok := run["spec"].(map[string]interface{}); ok {
		if overlays, ok := spec["overlays"].([]interface{}); ok && len(overlays) > 0 {
			fmt.Fprintf(w, "Overlays:   %d runtime overlay operations frozen in spec\n", len(overlays))
		}
		if swap, ok := spec["model_swap"].(map[string]interface{}); ok && boolVal(swap, "enabled") {
			fmt.Fprintf(w, "ModelSwap:  track=%s top=%.0f min_intel=%.2f max_cost=%.2f suggested=%.0f snapshot=%s\n",
				stringVal(swap, "track"),
				floatVal(swap, "top"),
				floatVal(swap, "min_intel"),
				floatVal(swap, "max_cost"),
				floatVal(swap, "total_suggested"),
				stringVal(swap, "repo_snapshot_at"),
			)
			if counts, ok := swap["candidate_counts"].(map[string]interface{}); ok && len(counts) > 0 {
				fmt.Fprintln(w, "  Candidate counts by model param:")
				keys := make([]string, 0, len(counts))
				for path := range counts {
					keys = append(keys, path)
				}
				sort.Strings(keys)
				for _, path := range keys {
					fmt.Fprintf(w, "  - %s: %.0f\n", path, floatVal(counts, path))
				}
			}
		}
	}
	if owner := stringVal(run, "owner_hostname"); owner != "" {
		fmt.Fprintf(w, "Owner:      %s:%d  (heartbeat %s)\n",
			owner,
			int(floatVal(run, "owner_pid")),
			heartbeatAgeString(time.Now().UTC(), stringVal(run, "last_heartbeat_at")),
		)
	}
	if started := stringVal(run, "started_at"); started != "" {
		fmt.Fprintf(w, "Started:    %s\n", started)
	}
	if completed := stringVal(run, "completed_at"); completed != "" {
		fmt.Fprintf(w, "Completed:  %s\n", completed)
	}

	timeline := deriveGenerationTimeline(run)
	if len(timeline) > 0 {
		fmt.Fprintln(w, "\nGENERATION TIMELINE:")
		parts := make([]string, 0, len(timeline))
		for _, point := range timeline {
			parts = append(parts, fmt.Sprintf("Gen %d: %.2f%%", point.gen, point.bestAcc*100))
		}
		fmt.Fprintln(w, strings.Join(parts, "  "))
	}
	if reason := deriveOptimizeStopReason(run, timeline); reason != "" {
		fmt.Fprintf(w, "\nSTOP REASON: %s\n", reason)
	}

	bestFitness, _ := run["best_fitness"].(map[string]interface{})
	if bestFitness == nil {
		fmt.Fprintln(w, "\nBest: none")
		return
	}

	fmt.Fprintln(w, "\nBest Fitness:")
	headers := []string{"SCORE", "ADJ_ACC", "ACC", "PARSE", "FLAGGED", "COST/ITEM", "LAT_MS", "P95_MS", "FEASIBLE"}
	rows := [][]string{{
		fmt.Sprintf("%.4f", floatVal(bestFitness, "composite_score")),
		fmt.Sprintf("%.2f%%", floatVal(bestFitness, "adjusted_accuracy")*100),
		fmt.Sprintf("%.2f%%", floatVal(bestFitness, "accuracy")*100),
		fmt.Sprintf("%.2f%%", floatVal(bestFitness, "parse_rate")*100),
		fmt.Sprintf("%.0f", floatVal(bestFitness, "flagged_items")),
		formatCostVal(floatVal(bestFitness, "cost_per_item")),
		fmt.Sprintf("%.0f", floatVal(bestFitness, "avg_latency_ms")),
		fmt.Sprintf("%.0f", floatVal(bestFitness, "p95_latency_ms")),
		fmt.Sprintf("%t", boolVal(bestFitness, "feasible")),
	}}
	printTableAuto(w, headers, rows, format)

	if violations, ok := bestFitness["constraint_violations"].([]interface{}); ok && len(violations) > 0 {
		fmt.Fprintln(w, "\nConstraint Violations:")
		for _, raw := range violations {
			if text, ok := raw.(string); ok && strings.TrimSpace(text) != "" {
				fmt.Fprintf(w, "- %s\n", text)
			}
		}
	}
}

func optimizeOrganismsTable(w io.Writer, data interface{}, format string) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return
	}
	organisms, ok := m["organisms"].([]interface{})
	if !ok {
		return
	}
	changeSummaries := readOptimizeChangeSummaries(m["__change_summaries"])
	showChanges := len(changeSummaries) > 0

	sort.SliceStable(organisms, func(i, j int) bool {
		left, lok := organisms[i].(map[string]interface{})
		right, rok := organisms[j].(map[string]interface{})
		if !lok || !rok {
			return false
		}
		lf := extractOrganismScore(left)
		rf := extractOrganismScore(right)
		if lf == rf {
			return stringVal(left, "id") < stringVal(right, "id")
		}
		return lf > rf
	})

	headers := []string{"ID", "GEN", "PARENTS", "MUTATION", "SCORE", "ADJ_ACC", "FLAGGED", "COST/ITEM", "FEASIBLE", "BENCH_RUN"}
	if showChanges {
		headers = append(headers, "CHANGES")
	}
	rows := make([][]string, 0, len(organisms))
	for _, raw := range organisms {
		org, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		fitness, _ := org["fitness"].(map[string]interface{})
		parentCount := 0
		if parents, ok := org["parent_ids"].([]interface{}); ok {
			parentCount = len(parents)
		}
		row := []string{
			stringVal(org, "id"),
			fmt.Sprintf("%.0f", floatVal(org, "generation")),
			fmt.Sprintf("%d", parentCount),
			stringVal(org, "mutation_type"),
			fmt.Sprintf("%.4f", getFitnessField(fitness, "composite_score")),
			fmt.Sprintf("%.2f%%", getFitnessField(fitness, "adjusted_accuracy")*100),
			fmt.Sprintf("%.0f", getFitnessField(fitness, "flagged_items")),
			formatCostVal(getFitnessField(fitness, "cost_per_item")),
			fmt.Sprintf("%t", getFitnessBool(fitness, "feasible")),
			truncate(stringVal(org, "bench_run_id"), 14),
		}
		if showChanges {
			row = append(row, coalesceVal(changeSummaries[stringVal(org, "id")], "-"))
		}
		rows = append(rows, row)
	}
	printTableAuto(w, headers, rows, format)
}

func optimizeDiffTable(w io.Writer, data interface{}, format string) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return
	}
	organism, _ := m["organism"].(map[string]interface{})
	paramChanges, _ := m["param_changes"].([]interface{})

	if organism != nil {
		fmt.Fprintf(w, "Organism:   %s\n", stringVal(organism, "id"))
		fmt.Fprintf(w, "Generation: %.0f\n", floatVal(organism, "generation"))
		fmt.Fprintf(w, "Mutation:   %s\n", stringVal(organism, "mutation_type"))
		if log := strings.TrimSpace(stringVal(organism, "mutation_log")); log != "" {
			fmt.Fprintf(w, "Reason:     %s\n", log)
		}
	}

	if len(paramChanges) == 0 {
		fmt.Fprintln(w, "\nNo parameter changes recorded.")
		return
	}
	fmt.Fprintln(w, "\nChanges:")

	headers := []string{"PATH", "OLD", "NEW", "REASON"}
	rows := make([][]string, 0, len(paramChanges))
	for _, raw := range paramChanges {
		change, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		path := strings.TrimSpace(stringVal(change, "path"))
		if path == "" {
			path = strings.TrimSpace(stringVal(change, "param_path"))
		}
		rows = append(rows, []string{
			path,
			truncate(stringVal(change, "old_value"), 36),
			truncate(stringVal(change, "new_value"), 36),
			truncate(stringVal(change, "reason"), 32),
		})
	}
	printTableAuto(w, headers, rows, format)
}

func optimizeLineageTable(w io.Writer, data interface{}, format string) {
	_ = format
	m, ok := data.(map[string]interface{})
	if !ok {
		return
	}
	nodesRaw, _ := m["nodes"].([]interface{})
	edgesRaw, _ := m["edges"].([]interface{})
	if len(nodesRaw) == 0 {
		if organismsRaw, ok := m["organisms"].([]interface{}); ok && len(organismsRaw) > 0 {
			printOrganismAncestryTable(w, organismsRaw)
			return
		}
	}
	if len(nodesRaw) == 0 {
		fmt.Fprintln(w, "(no lineage data)")
		return
	}

	nodes := make(map[string]lineageNode, len(nodesRaw))
	inDegree := make(map[string]int, len(nodesRaw))
	children := make(map[string][]string, len(nodesRaw))
	for _, raw := range nodesRaw {
		nm, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		id := stringVal(nm, "id")
		if id == "" {
			continue
		}
		node := lineageNode{
			id:       id,
			gen:      int(floatVal(nm, "generation")),
			score:    floatVal(nm, "composite_score"),
			feasible: boolVal(nm, "feasible"),
		}
		nodes[id] = node
		inDegree[id] = 0
	}
	for _, raw := range edgesRaw {
		edge, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		from := stringVal(edge, "from")
		to := stringVal(edge, "to")
		if from == "" || to == "" {
			continue
		}
		children[from] = append(children[from], to)
		inDegree[to]++
	}
	for parentID := range children {
		sort.Slice(children[parentID], func(i, j int) bool {
			li := nodes[children[parentID][i]]
			lj := nodes[children[parentID][j]]
			if li.gen == lj.gen {
				return li.id < lj.id
			}
			return li.gen < lj.gen
		})
	}

	roots := make([]lineageNode, 0)
	for id, node := range nodes {
		if inDegree[id] == 0 {
			roots = append(roots, node)
		}
	}
	sort.Slice(roots, func(i, j int) bool {
		if roots[i].gen == roots[j].gen {
			return roots[i].id < roots[j].id
		}
		return roots[i].gen < roots[j].gen
	})

	visited := make(map[string]bool, len(nodes))
	for idx, root := range roots {
		last := idx == len(roots)-1
		printLineageNode(w, root, "", last)
		visited[root.id] = true
		printLineageChildren(w, root.id, children, nodes, visited, "", last)
	}
}

func printLineageChildren(w io.Writer, parentID string, children map[string][]string, nodes map[string]lineageNode, visited map[string]bool, prefix string, parentLast bool) {
	nextPrefix := prefix
	if parentLast {
		nextPrefix += "   "
	} else {
		nextPrefix += "│  "
	}
	kids := children[parentID]
	for idx, childID := range kids {
		child, ok := nodes[childID]
		if !ok {
			continue
		}
		last := idx == len(kids)-1
		printLineageNode(w, child, nextPrefix, last)
		if visited[childID] {
			continue
		}
		visited[childID] = true
		printLineageChildren(w, childID, children, nodes, visited, nextPrefix, last)
	}
}

func printLineageNode(w io.Writer, node lineageNode, prefix string, last bool) {
	branch := "├─ "
	if last {
		branch = "└─ "
	}
	feasible := "!"
	if node.feasible {
		feasible = "✓"
	}
	fmt.Fprintf(w, "%s%s%s [g%d score=%.4f %s]\n", prefix, branch, node.id, node.gen, node.score, feasible)
}

func optimizeLearningLogTable(w io.Writer, data interface{}, format string) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return
	}
	entries, ok := m["entries"].([]interface{})
	if !ok {
		return
	}
	headers := []string{"GEN", "ORGANISM", "PARENT", "TYPE", "VERIFY", "OUTCOME", "DELTA", "DESCRIPTION"}
	rows := make([][]string, 0, len(entries))
	counts := map[string]int{
		"improvement":          0,
		"regression":           0,
		"no_change":            0,
		"constraint_violation": 0,
	}
	for _, raw := range entries {
		entry, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		outcome := strings.ToLower(strings.TrimSpace(stringVal(entry, "outcome")))
		if _, exists := counts[outcome]; exists {
			counts[outcome]++
		}
		rows = append(rows, []string{
			fmt.Sprintf("%.0f", floatVal(entry, "generation")),
			truncate(stringVal(entry, "organism_id"), 12),
			truncate(stringVal(entry, "parent_id"), 12),
			stringVal(entry, "mutation_type"),
			coalesceVal(stringVal(entry, "verify_method"), "-"),
			outcome,
			fmt.Sprintf("%+.4f", floatVal(entry, "fitness_delta")),
			truncate(stringVal(entry, "description"), 60),
		})
	}
	printTableAuto(w, headers, rows, format)
	fmt.Fprintf(
		w,
		"\nSummary: %d entries | %d improvements, %d regressions, %d no_change, %d violations\n",
		len(rows),
		counts["improvement"],
		counts["regression"],
		counts["no_change"],
		counts["constraint_violation"],
	)
}

func optimizeCompareTable(w io.Writer, data interface{}, format string) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return
	}
	runs, _ := m["runs"].([]interface{})
	if len(runs) == 0 {
		fmt.Fprintln(w, "(no runs)")
		return
	}
	controlID := strings.TrimSpace(stringVal(m, "__control_id"))
	if controlID == "" {
		controlID = deriveOptimizeControlRunID(m)
	}
	controlAcc := 0.0
	for _, raw := range runs {
		run, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if strings.TrimSpace(stringVal(run, "id")) != controlID {
			continue
		}
		bestFitness, _ := run["best_fitness"].(map[string]interface{})
		controlAcc = floatVal(bestFitness, "adjusted_accuracy")
		break
	}

	headers := []string{"RUN", "MUTATOR", "GENS", "ORGS", "BEST_ACC", "BUDGET", "ACC_DELTA", "EFFICIENCY", "PARSE", "COST/ITEM", "FEASIBLE%"}
	rows := make([][]string, 0, len(runs))
	runOrder := make([]string, 0, len(runs))
	for _, raw := range runs {
		run, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		runID := stringVal(run, "id")
		runOrder = append(runOrder, runID)
		bestFitness, _ := run["best_fitness"].(map[string]interface{})
		bestAcc := floatVal(bestFitness, "adjusted_accuracy")
		parseRate := floatVal(bestFitness, "parse_rate")
		costPerItem := floatVal(bestFitness, "cost_per_item")
		accDelta := "-"
		if controlID != "" && runID != controlID {
			accDelta = fmt.Sprintf("%+.2fpp", (bestAcc-controlAcc)*100)
		}
		efficiency := "-"
		spent := floatVal(run, "spent_usd")
		if spent > 0 {
			efficiency = fmt.Sprintf("%.2fpp/$", (bestAcc*100)/spent)
		}
		feasiblePct := "-"
		if totalItems := floatVal(bestFitness, "total_items"); totalItems > 0 {
			failedItems := floatVal(bestFitness, "failed_items")
			feasiblePct = fmt.Sprintf("%.1f%%", ((totalItems-failedItems)/totalItems)*100)
		}
		if runID == controlID {
			runID += "*"
		}
		rows = append(rows, []string{
			runID,
			optimize.NormalizeMutatorMode(coalesceVal(stringVal(run, "mutator_mode"), optimize.MutatorModeAuto)),
			fmt.Sprintf("%.0f", floatVal(run, "generation")),
			fmt.Sprintf("%.0f", floatVal(run, "total_organisms")),
			fmt.Sprintf("%.2f%%", bestAcc*100),
			formatCostVal(floatVal(run, "spent_usd")),
			accDelta,
			efficiency,
			fmt.Sprintf("%.2f%%", parseRate*100),
			formatCostVal(costPerItem),
			feasiblePct,
		})
	}

	fmt.Fprintf(w, "COMPARISON: %d runs\n\n", len(rows))
	printTableAuto(w, headers, rows, format)
	if controlID != "" {
		fmt.Fprintf(w, "\n* = control run (%s)\n", controlID)
	}

	bestOrganisms, _ := m["best_organisms"].([]interface{})
	if len(bestOrganisms) == 0 || len(runOrder) == 0 {
		return
	}
	byRun := make(map[string]map[string]interface{}, len(bestOrganisms))
	for _, raw := range bestOrganisms {
		organism, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		runID := strings.TrimSpace(stringVal(organism, "opt_run_id"))
		if runID != "" {
			byRun[runID] = organism
		}
	}
	paths := make([]string, 0)
	seenPaths := make(map[string]struct{})
	for _, runID := range runOrder {
		organism := byRun[runID]
		if organism == nil {
			continue
		}
		paramValues, ok := organism["param_values"].(map[string]interface{})
		if !ok {
			continue
		}
		for path := range paramValues {
			if _, exists := seenPaths[path]; exists {
				continue
			}
			seenPaths[path] = struct{}{}
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return
	}
	if len(paths) > 30 {
		paths = paths[:30]
	}

	fmt.Fprintln(w, "\nBEST ORGANISM PARAMETER DIFF:")
	diffHeaders := []string{"PATH"}
	diffHeaders = append(diffHeaders, runOrder...)
	diffRows := make([][]string, 0, len(paths))
	for _, path := range paths {
		row := []string{path}
		for _, runID := range runOrder {
			value := "-"
			organism := byRun[runID]
			if organism != nil {
				if paramValues, ok := organism["param_values"].(map[string]interface{}); ok {
					if raw, exists := paramValues[path]; exists {
						value = truncate(fmt.Sprintf("%v", raw), 48)
					}
				}
			}
			row = append(row, value)
		}
		diffRows = append(diffRows, row)
	}
	printTableAuto(w, diffHeaders, diffRows, format)
}

func optimizeArtifactsTable(w io.Writer, data interface{}, format string) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return
	}
	organismID := strings.TrimSpace(stringVal(m, "__organism_id"))
	if organismID != "" {
		fmt.Fprintf(w, "ORGANISM: %s\n", organismID)
	}
	artifacts, _ := m["artifacts"].([]interface{})
	if len(artifacts) == 0 {
		fmt.Fprintln(w, "(no mutation artifacts)")
		return
	}

	for i, raw := range artifacts {
		artifact, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		fmt.Fprintf(w, "\nArtifact %d\n", i+1)
		fmt.Fprintf(w, "CLAUDE MODEL: %s\n", coalesceVal(stringVal(artifact, "claude_model"), "-"))
		if prompt := strings.TrimSpace(stringVal(artifact, "input_prompt")); prompt != "" {
			fmt.Fprintf(w, "--- INPUT PROMPT (hash: %s) ---\n", coalesceVal(stringVal(artifact, "input_prompt_hash"), "-"))
			fmt.Fprintln(w, prompt)
		} else {
			fmt.Fprintf(w, "--- INPUT PROMPT HASH: %s ---\n", coalesceVal(stringVal(artifact, "input_prompt_hash"), "-"))
		}
		if output := strings.TrimSpace(stringVal(artifact, "raw_output")); output != "" {
			fmt.Fprintf(w, "--- RAW OUTPUT (hash: %s) ---\n", coalesceVal(stringVal(artifact, "raw_output_hash"), "-"))
			fmt.Fprintln(w, output)
		} else {
			fmt.Fprintf(w, "--- RAW OUTPUT HASH: %s ---\n", coalesceVal(stringVal(artifact, "raw_output_hash"), "-"))
		}
	}
}

func optimizeSpecTable(w io.Writer, data interface{}, format string) {
	run, ok := data.(map[string]interface{})
	if !ok {
		return
	}
	fmt.Fprintf(w, "RUN: %s\n", stringVal(run, "id"))
	fmt.Fprintf(w, "WORKFLOW: %s\n\n", stringVal(run, "workflow_id"))
	spec, _ := run["spec"].(map[string]interface{})
	if spec == nil {
		fmt.Fprintln(w, "(no spec)")
		return
	}

	params, _ := spec["params"].([]interface{})
	fmt.Fprintf(w, "PARAMETERS (%d):\n", len(params))
	paramRows := make([][]string, 0, len(params))
	for _, raw := range params {
		param, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		candidates := "-"
		if cands, ok := param["candidates"].([]interface{}); ok && len(cands) > 0 {
			items := make([]string, 0, len(cands))
			for _, cand := range cands {
				items = append(items, fmt.Sprintf("%v", cand))
			}
			candidates = strings.Join(items, ", ")
		}
		if rng, ok := param["range"].(map[string]interface{}); ok {
			candidates = fmt.Sprintf(
				"[%.3f, %.3f] step=%.3f",
				floatVal(rng, "min"),
				floatVal(rng, "max"),
				floatVal(rng, "step"),
			)
		}
		paramRows = append(paramRows, []string{
			stringVal(param, "path"),
			stringVal(param, "type"),
			candidates,
			coalesceVal(stringVal(param, "group"), "-"),
		})
	}
	printTableAuto(w, []string{"PATH", "TYPE", "CANDIDATES/RANGE", "GROUP"}, paramRows, format)

	locked, _ := spec["locked"].([]interface{})
	fmt.Fprintf(w, "\nLOCKED (%d):\n", len(locked))
	for _, raw := range locked {
		fmt.Fprintf(w, "  %v\n", raw)
	}

	if objectives, ok := spec["objectives"].([]interface{}); ok && len(objectives) > 0 {
		fmt.Fprintln(w, "\nOBJECTIVES:")
		rows := make([][]string, 0, len(objectives))
		for _, raw := range objectives {
			obj, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			rows = append(rows, []string{
				stringVal(obj, "metric"),
				stringVal(obj, "direction"),
				fmt.Sprintf("%.2f", floatVal(obj, "weight")),
			})
		}
		printTableAuto(w, []string{"METRIC", "DIR", "WEIGHT"}, rows, format)
	}

	if constraints, ok := spec["constraints"].([]interface{}); ok && len(constraints) > 0 {
		fmt.Fprintln(w, "\nCONSTRAINTS:")
		rows := make([][]string, 0, len(constraints))
		for _, raw := range constraints {
			con, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			rows = append(rows, []string{
				stringVal(con, "metric"),
				stringVal(con, "op"),
				fmt.Sprintf("%.3f", floatVal(con, "value")),
			})
		}
		printTableAuto(w, []string{"METRIC", "OP", "VALUE"}, rows, format)
	}

	if stopPolicy, ok := spec["stop_policy"].(map[string]interface{}); ok {
		fmt.Fprintln(w, "\nSTOP POLICY:")
		fmt.Fprintf(w, "  Max generations: %.0f\n", floatVal(stopPolicy, "max_generations"))
		fmt.Fprintf(w, "  Budget: %s\n", formatCostVal(floatVal(stopPolicy, "budget_usd")))
		fmt.Fprintf(w, "  Plateau: %.0f generations\n", floatVal(stopPolicy, "plateau_generations"))
		fmt.Fprintf(w, "  Stability top-K: %.0f\n", floatVal(stopPolicy, "stability_top_k"))
	}
	if promotionPolicy, ok := spec["promotion_policy"].(map[string]interface{}); ok {
		fmt.Fprintln(w, "\nPROMOTION POLICY:")
		fmt.Fprintf(w, "  Min accuracy gain: %.2fpp\n", floatVal(promotionPolicy, "min_accuracy_gain")*100)
		if metrics, ok := promotionPolicy["no_regression_on"].([]interface{}); ok {
			names := make([]string, 0, len(metrics))
			for _, metric := range metrics {
				names = append(names, fmt.Sprintf("%v", metric))
			}
			fmt.Fprintf(w, "  No regression on: %s\n", strings.Join(names, ", "))
		}
		fmt.Fprintf(w, "  Generalization check required: %t\n", boolVal(promotionPolicy, "require_generalization_check"))
	}
}

type optimizeGenerationPoint struct {
	gen     int
	bestAcc float64
}

func deriveGenerationTimeline(run map[string]interface{}) []optimizeGenerationPoint {
	organismsRaw, _ := run["__organisms"].([]interface{})
	if len(organismsRaw) == 0 {
		return nil
	}
	bestByGen := make(map[int]float64)
	for _, raw := range organismsRaw {
		org, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		generation := int(floatVal(org, "generation"))
		fitness, _ := org["fitness"].(map[string]interface{})
		if fitness == nil {
			continue
		}
		acc := floatVal(fitness, "adjusted_accuracy")
		prev, exists := bestByGen[generation]
		if !exists || acc > prev {
			bestByGen[generation] = acc
		}
	}
	if len(bestByGen) == 0 {
		return nil
	}
	generations := make([]int, 0, len(bestByGen))
	for generation := range bestByGen {
		generations = append(generations, generation)
	}
	sort.Ints(generations)
	out := make([]optimizeGenerationPoint, 0, len(generations))
	for _, generation := range generations {
		out = append(out, optimizeGenerationPoint{gen: generation, bestAcc: bestByGen[generation]})
	}
	return out
}

func deriveOptimizeStopReason(run map[string]interface{}, timeline []optimizeGenerationPoint) string {
	status := strings.ToLower(strings.TrimSpace(stringVal(run, "status")))
	if status == "running" || status == "pending" || status == "paused" {
		return ""
	}
	spec, _ := run["spec"].(map[string]interface{})
	stopPolicy, _ := spec["stop_policy"].(map[string]interface{})
	maxGens := int(floatVal(stopPolicy, "max_generations"))
	plateauGens := int(floatVal(stopPolicy, "plateau_generations"))
	budget := floatVal(stopPolicy, "budget_usd")
	spent := floatVal(run, "spent_usd")
	currentGen := int(floatVal(run, "generation"))

	if budget > 0 && spent >= budget {
		return "budget exhausted"
	}
	if maxGens > 0 && currentGen >= maxGens {
		return fmt.Sprintf("max generations reached (%d)", maxGens)
	}
	if plateauGens > 0 && len(timeline) > 1 {
		lastImprovementGen := timeline[0].gen
		bestAcc := timeline[0].bestAcc
		for _, point := range timeline[1:] {
			if point.bestAcc > bestAcc {
				bestAcc = point.bestAcc
				lastImprovementGen = point.gen
			}
		}
		if currentGen-lastImprovementGen >= plateauGens {
			return fmt.Sprintf("plateau detected (%d generations without improvement)", plateauGens)
		}
	}
	switch status {
	case "failed":
		return "run failed"
	case "cancelled":
		return "run cancelled"
	case "completed":
		return "completed by stop policy"
	default:
		return status
	}
}

func printOrganismAncestryTable(w io.Writer, organismsRaw []interface{}) {
	organisms := make([]map[string]interface{}, 0, len(organismsRaw))
	for _, raw := range organismsRaw {
		organism, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		organisms = append(organisms, organism)
	}
	if len(organisms) == 0 {
		fmt.Fprintln(w, "(no ancestry data)")
		return
	}
	sort.Slice(organisms, func(i, j int) bool {
		li := int(floatVal(organisms[i], "generation"))
		lj := int(floatVal(organisms[j], "generation"))
		if li == lj {
			return stringVal(organisms[i], "id") < stringVal(organisms[j], "id")
		}
		return li < lj
	})
	for idx, organism := range organisms {
		fitness, _ := organism["fitness"].(map[string]interface{})
		score := floatVal(fitness, "composite_score")
		acc := floatVal(fitness, "adjusted_accuracy")
		prefix := "└─"
		if idx < len(organisms)-1 {
			prefix = "├─"
		}
		fmt.Fprintf(
			w,
			"%s %s [g%d score=%.4f acc=%.2f%%]\n",
			prefix,
			stringVal(organism, "id"),
			int(floatVal(organism, "generation")),
			score,
			acc*100,
		)
	}
}

func readOptimizeChangeSummaries(raw interface{}) map[string]string {
	switch value := raw.(type) {
	case map[string]string:
		return value
	case map[string]interface{}:
		out := make(map[string]string, len(value))
		for key, summary := range value {
			out[key] = fmt.Sprintf("%v", summary)
		}
		return out
	default:
		return map[string]string{}
	}
}

func extractOrganismScore(org map[string]interface{}) float64 {
	fitness, _ := org["fitness"].(map[string]interface{})
	if fitness == nil {
		return 0
	}
	return floatVal(fitness, "composite_score")
}

func getFitnessField(fitness map[string]interface{}, key string) float64 {
	if fitness == nil {
		return 0
	}
	return floatVal(fitness, key)
}

func getFitnessBool(fitness map[string]interface{}, key string) bool {
	if fitness == nil {
		return false
	}
	return boolVal(fitness, key)
}

func heartbeatAgeString(now time.Time, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "-"
	}
	ts, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		// DB JSON can surface sqlite datetime format.
		ts, err = time.Parse("2006-01-02 15:04:05", raw)
		if err != nil {
			return raw
		}
	}
	age := now.Sub(ts.UTC())
	if age < 0 {
		age = 0
	}
	if age < time.Second {
		return "<1s"
	}
	if age < time.Minute {
		return fmt.Sprintf("%ds", int(age.Seconds()))
	}
	return age.Truncate(time.Second).String()
}

func coalesceVal(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
