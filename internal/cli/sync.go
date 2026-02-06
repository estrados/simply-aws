package cli

import (
	"fmt"
	"time"

	"github.com/estrados/simply-aws/internal/sync"
)

// RunSync syncs all AWS resources for the given region and prints progress.
func RunSync(region string) {
	start := time.Now()
	fmt.Printf("%s  %s\n\n", bold("saws sync"), dim(region))

	step := func(label string) {
		fmt.Printf("  %s %s\n", green("✓"), label)
	}

	// Network
	printSyncSection("Network", func() ([]sync.SyncResult, error) {
		return sync.SyncVPCData(region, step)
	})

	// S3 & Data
	printSyncSection("S3 & Data", func() ([]sync.SyncResult, error) {
		var all []sync.SyncResult
		if r, err := sync.SyncS3WithRegions(step); err == nil {
			all = append(all, *r)
		} else {
			all = append(all, sync.SyncResult{Service: "s3", Error: err.Error()})
		}
		dw, err := sync.SyncDataWarehouseData(region, step)
		if err == nil {
			all = append(all, dw...)
		}
		return all, nil
	})

	// Database
	printSyncSection("Database", func() ([]sync.SyncResult, error) {
		return sync.SyncDatabaseData(region, step)
	})

	// Compute
	printSyncSection("Compute", func() ([]sync.SyncResult, error) {
		return sync.SyncComputeData(region, step)
	})

	// Streaming
	printSyncSection("Queues & Streaming", func() ([]sync.SyncResult, error) {
		return sync.SyncStreamingData(region, step)
	})

	// AI
	printSyncSection("AI & ML", func() ([]sync.SyncResult, error) {
		return sync.SyncAIData(region, step)
	})

	// IAM (global)
	printSyncSection("IAM", func() ([]sync.SyncResult, error) {
		return sync.SyncIAMData(step)
	})

	elapsed := time.Since(start).Round(time.Millisecond)
	fmt.Printf("\n%s in %s\n", bold("Done"), dim(elapsed.String()))
}

func printSyncSection(name string, fn func() ([]sync.SyncResult, error)) {
	fmt.Printf("%s\n", bold("━━ "+name))
	results, err := fn()
	if err != nil {
		fmt.Printf("  %s %s\n", red("✗"), err.Error())
		return
	}

	total := 0
	errors := 0
	for _, r := range results {
		if r.Error != "" {
			errors++
			fmt.Printf("  %s %s: %s\n", red("✗"), r.Service, dim(r.Error))
		} else {
			total += r.Count
		}
	}

	if errors == 0 {
		fmt.Printf("  %s %d resources\n", cyan("→"), total)
	}
	fmt.Println()
}
