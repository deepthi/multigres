// Copyright 2025 The Multigres Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cluster

import (
	"context"
	"fmt"
	"time"

	"github.com/multigres/multigres/go/cmd/multigres/internal/adminclient"

	"github.com/spf13/cobra"
)

// stop handles the stopping of a multigres cluster
func stop(cmd *cobra.Command, args []string) error {
	// Get admin server endpoint
	adminEndpoint, err := cmd.Flags().GetString("admin-endpoint")
	if err != nil {
		return fmt.Errorf("failed to get admin-endpoint flag: %w", err)
	}

	// Get config paths from flags
	configPaths, err := cmd.Flags().GetStringSlice("config-path")
	if err != nil {
		return fmt.Errorf("failed to get config-path flag: %w", err)
	}
	if len(configPaths) == 0 {
		configPaths = []string{"."}
	}

	// Get clean flag
	clean, err := cmd.Flags().GetBool("clean")
	if err != nil {
		return fmt.Errorf("failed to get clean flag: %w", err)
	}

	fmt.Println("Stopping Multigres cluster...")

	// Create admin client
	client, err := adminclient.NewClient(adminEndpoint)
	if err != nil {
		return fmt.Errorf("failed to connect to admin server at %s: %w", adminEndpoint, err)
	}
	defer client.Close()

	// Stop cluster
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	resp, err := client.StopCluster(ctx, configPaths, "", clean)
	if err != nil {
		return fmt.Errorf("failed to stop cluster: %w", err)
	}

	fmt.Printf("✅ %s\n", resp.Message)

	if len(resp.ServicesStopped) > 0 {
		fmt.Println("\nStopped services:")
		for _, service := range resp.ServicesStopped {
			fmt.Printf("  - %s\n", service)
		}
	}

	if clean {
		fmt.Println("🧹 All cluster data has been cleaned up")
	}

	return nil
}

var StopCommand = &cobra.Command{
	Use:   "stop",
	Short: "Stop local cluster",
	Long:  "Stop the local Multigres cluster. Use --clean to fully tear down all resources.",
	RunE:  stop,
}

func init() {
	StopCommand.Flags().Bool("clean", false, "Fully tear down all cluster resources")
	StopCommand.Flags().String("admin-endpoint", "localhost:15990", "Admin server endpoint")
}
