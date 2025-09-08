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
	"strings"
	"time"

	"github.com/multigres/multigres/go/cmd/multigres/internal/adminclient"
	adminpb "github.com/multigres/multigres/go/pb/adminservice"

	"github.com/spf13/cobra"
)

// status handles showing the status of a multigres cluster
func status(cmd *cobra.Command, args []string) error {
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

	// Create admin client
	client, err := adminclient.NewClient(adminEndpoint)
	if err != nil {
		return fmt.Errorf("failed to connect to admin server at %s: %w", adminEndpoint, err)
	}
	defer client.Close()

	// Get cluster status
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.GetClusterStatus(ctx, configPaths, "")
	if err != nil {
		return fmt.Errorf("failed to get cluster status: %w", err)
	}

	// Print status
	printClusterStatus(resp)

	return nil
}

// printClusterStatus prints the cluster status in a human-readable format
func printClusterStatus(status *adminpb.GetClusterStatusResponse) {
	fmt.Println(strings.Repeat("=", 50))
	fmt.Printf("Cluster Status: %s\n", formatClusterStatus(status.Status))
	fmt.Println(strings.Repeat("=", 50))

	if status.Message != "" {
		fmt.Printf("Message: %s\n\n", status.Message)
	}

	if len(status.Services) == 0 {
		fmt.Println("No services running")
		return
	}

	fmt.Println("\nServices:")
	fmt.Println("---------")

	for _, service := range status.Services {
		fmt.Printf("\n%s\n", service.ServiceInfo.Name)
		fmt.Printf("  Status: %s\n", formatServiceStatus(service.Status))
		fmt.Printf("  Health: %s\n", formatHealthStatus(service.Health))
		fmt.Printf("  Host:   %s\n", service.ServiceInfo.Fqdn)

		if len(service.ServiceInfo.Ports) > 0 {
			fmt.Printf("  Ports:  ")
			var portStrs []string
			for name, port := range service.ServiceInfo.Ports {
				portStrs = append(portStrs, fmt.Sprintf("%s:%d", name, port))
			}
			fmt.Printf("%s\n", strings.Join(portStrs, ", "))
		}

		if service.ErrorMessage != "" {
			fmt.Printf("  Error:  %s\n", service.ErrorMessage)
		}

		lastCheck := time.Unix(service.LastCheckTime, 0)
		fmt.Printf("  Last Check: %s\n", lastCheck.Format("2006-01-02 15:04:05"))
	}

	fmt.Println(strings.Repeat("=", 50))
}

// formatClusterStatus returns a human-readable cluster status
func formatClusterStatus(status adminpb.ClusterStatus) string {
	switch status {
	case adminpb.ClusterStatus_CLUSTER_STATUS_RUNNING:
		return "🟢 RUNNING"
	case adminpb.ClusterStatus_CLUSTER_STATUS_STOPPED:
		return "🔴 STOPPED"
	case adminpb.ClusterStatus_CLUSTER_STATUS_STARTING:
		return "🟡 STARTING"
	case adminpb.ClusterStatus_CLUSTER_STATUS_STOPPING:
		return "🟡 STOPPING"
	case adminpb.ClusterStatus_CLUSTER_STATUS_ERROR:
		return "🔴 ERROR"
	default:
		return "❓ UNKNOWN"
	}
}

// formatServiceStatus returns a human-readable service status
func formatServiceStatus(status adminpb.ServiceState) string {
	switch status {
	case adminpb.ServiceState_SERVICE_STATE_RUNNING:
		return "🟢 RUNNING"
	case adminpb.ServiceState_SERVICE_STATE_STOPPED:
		return "🔴 STOPPED"
	case adminpb.ServiceState_SERVICE_STATE_STARTING:
		return "🟡 STARTING"
	case adminpb.ServiceState_SERVICE_STATE_STOPPING:
		return "🟡 STOPPING"
	case adminpb.ServiceState_SERVICE_STATE_ERROR:
		return "🔴 ERROR"
	default:
		return "❓ UNKNOWN"
	}
}

// formatHealthStatus returns a human-readable health status
func formatHealthStatus(health adminpb.HealthStatus) string {
	switch health {
	case adminpb.HealthStatus_HEALTH_STATUS_HEALTHY:
		return "✅ HEALTHY"
	case adminpb.HealthStatus_HEALTH_STATUS_UNHEALTHY:
		return "❌ UNHEALTHY"
	case adminpb.HealthStatus_HEALTH_STATUS_UNKNOWN:
		return "❓ UNKNOWN"
	default:
		return "❓ UNSPECIFIED"
	}
}

var StatusCommand = &cobra.Command{
	Use:   "status",
	Short: "Show cluster health",
	Long:  "Display the current health and status of the Multigres cluster.",
	RunE:  status,
}

func init() {
	StatusCommand.Flags().String("admin-endpoint", "localhost:15990", "Admin server endpoint")
}
