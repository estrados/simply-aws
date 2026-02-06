package main

import (
	"fmt"
	"log"
	"os"

	"github.com/estrados/simply-aws/internal/awscli"
	"github.com/estrados/simply-aws/internal/cli"
	"github.com/estrados/simply-aws/internal/server"
	"github.com/estrados/simply-aws/internal/sync"
	"github.com/spf13/cobra"
)

func main() {
	var port int

	rootCmd := &cobra.Command{
		Use:   "saws",
		Short: "simply-aws — local-first AWS infrastructure designer",
	}

	upCmd := &cobra.Command{
		Use:   "up",
		Short: "Start the saws web server",
		Run: func(cmd *cobra.Command, args []string) {
			if err := sync.InitDB(); err != nil {
				log.Fatalf("failed to init database: %v", err)
			}
			defer sync.CloseDB()

			status := awscli.Detect()
			if status.Installed {
				fmt.Printf("AWS CLI detected: %s\n", status.Version)
				fmt.Printf("Region: %s | Account: %s\n", status.Region, status.AccountID)
			} else {
				fmt.Println("AWS CLI not found — sync features will be unavailable")
			}

			addr := fmt.Sprintf(":%d", port)
			fmt.Printf("\nsaws is running at http://localhost%s\n", addr)

			if err := server.Start(addr, status); err != nil {
				log.Fatal(err)
			}
		},
	}

	upCmd.Flags().IntVarP(&port, "port", "p", 3131, "port to listen on")

	var viewRegion string
	viewCmd := &cobra.Command{
		Use:   "view",
		Short: "Interactive terminal view of cached AWS infrastructure",
		Run: func(cmd *cobra.Command, args []string) {
			if err := sync.InitDB(); err != nil {
				log.Fatalf("failed to init database: %v", err)
			}
			defer sync.CloseDB()

			region := viewRegion
			if region == "" {
				status := awscli.Detect()
				region = status.Region
			}
			if region == "" {
				region = "us-east-1"
			}

			cli.RunView(region)
		},
	}
	viewCmd.Flags().StringVar(&viewRegion, "region", "", "AWS region to view")

	var syncRegion string
	syncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync AWS infrastructure to local cache",
		Run: func(cmd *cobra.Command, args []string) {
			if err := sync.InitDB(); err != nil {
				log.Fatalf("failed to init database: %v", err)
			}
			defer sync.CloseDB()

			status := awscli.Detect()
			if !status.Installed {
				log.Fatal("AWS CLI not found — cannot sync")
			}

			region := syncRegion
			if region == "" {
				region = status.Region
			}
			if region == "" {
				region = "us-east-1"
			}

			cli.RunSync(region)
		},
	}
	syncCmd.Flags().StringVar(&syncRegion, "region", "", "AWS region to sync")

	rootCmd.AddCommand(upCmd, viewCmd, syncCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
