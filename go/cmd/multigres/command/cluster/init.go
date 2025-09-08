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

// runInit handles the initialization of a multigres cluster configuration
func runInit(cmd *cobra.Command, args []string) error {
	// Get admin server endpoint
	adminEndpoint, err := cmd.Flags().GetString("admin-endpoint")
	if err != nil {
		return fmt.Errorf("failed to get admin-endpoint flag: %w", err)
	}

	// Get config paths
	configPaths, err := cmd.Flags().GetStringSlice("config-path")
	if err != nil {
		return fmt.Errorf("failed to get config-path flag: %w", err)
	}

	// Get provisioner
	provisioner, err := cmd.Flags().GetString("provisioner")
	if err != nil {
		return fmt.Errorf("failed to get provisioner flag: %w", err)
	}

	fmt.Println("Initializing Multigres cluster configuration...")

	// Create admin client
	client, err := adminclient.NewClient(adminEndpoint)
	if err != nil {
		return fmt.Errorf("failed to connect to admin server at %s: %w", adminEndpoint, err)
	}
	defer client.Close()

	// Initialize cluster via gRPC
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.InitCluster(ctx, configPaths, provisioner)
	if err != nil {
		return fmt.Errorf("failed to initialize cluster: %w", err)
	}

	fmt.Printf("Created configuration file: %s\n", resp.ConfigFilePath)
	fmt.Printf("Cluster ID: %s\n", resp.ClusterId)
	fmt.Println("Cluster configuration created successfully!")
	return nil
}

var InitCommand = &cobra.Command{
	Use:   "init",
	Short: "Create a local cluster configuration",
	Long:  "Initialize a new local Multigres cluster configuration that can be used with 'multigres cluster start'.",
	RunE:  runInit,
}

func init() {
	InitCommand.Flags().String("provisioner", "local", "Provisioner to use (only 'local' is supported)")
	InitCommand.Flags().String("admin-endpoint", "localhost:15990", "Admin server endpoint")
}
