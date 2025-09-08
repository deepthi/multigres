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
	"github.com/multigres/multigres/go/servenv"

	"github.com/spf13/cobra"
)

// start handles the starting of a multigres cluster
func start(cmd *cobra.Command, args []string) error {
	servenv.FireRunHooks()

	fmt.Println("Multigres — Distributed Postgres made easy")
	fmt.Println("=================================================================")
	fmt.Println("✨ Bootstrapping your local Multigres cluster — this may take a few moments ✨")

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

	// Track services for final summary
	var services []*adminpb.ServiceInfo

	// Start cluster with streaming updates
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	err = client.StartCluster(ctx, configPaths, "", func(resp *adminpb.StartClusterResponse) error {
		switch resp.EventType {
		case adminpb.StartEventType_START_EVENT_TYPE_STARTING:
			fmt.Printf("📡 %s\n", resp.Message)

		case adminpb.StartEventType_START_EVENT_TYPE_SERVICE_PROVISIONED:
			fmt.Printf("✅ %s\n", resp.Message)
			if resp.Service != nil {
				services = append(services, resp.Service)
			}

		case adminpb.StartEventType_START_EVENT_TYPE_COMPLETED:
			fmt.Printf("🎉 %s\n", resp.Message)

		case adminpb.StartEventType_START_EVENT_TYPE_ERROR:
			return fmt.Errorf("cluster start failed: %s", resp.Error)
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to start cluster: %w", err)
	}

	// Print service summary
	printServiceSummary(services)

	return nil
}

// printServiceSummary prints a summary of the provisioned services
func printServiceSummary(services []*adminpb.ServiceInfo) {
	fmt.Println(strings.Repeat("=", 65))
	fmt.Println("🎉 - Multigres cluster started successfully!")
	fmt.Println(strings.Repeat("=", 65))
	fmt.Println()
	fmt.Println("Provisioned Services")
	fmt.Println("--------------------")
	fmt.Println()

	for _, service := range services {
		fmt.Printf("%s\n", service.Name)
		fmt.Printf("   Host: %s\n", service.Fqdn)

		if len(service.Ports) == 1 {
			// Single port format
			for portName, portNum := range service.Ports {
				if portName == "http_port" {
					fmt.Printf("   Port: %d → http://%s:%d\n", portNum, service.Fqdn, portNum)
				} else {
					fmt.Printf("   Port: %d\n", portNum)
				}
			}
		} else if len(service.Ports) > 1 {
			// Multiple ports format
			fmt.Printf("   Ports:\n")
			for portName, portNum := range service.Ports {
				displayPortName := strings.ToUpper(strings.Replace(portName, "_port", "", 1))
				if portName == "http_port" {
					fmt.Printf("     - %s: %d → http://%s:%d\n", displayPortName, portNum, service.Fqdn, portNum)
				} else {
					fmt.Printf("     - %s: %d\n", displayPortName, portNum)
				}
			}
		}

		if logFile, ok := service.Metadata["log_file"]; ok {
			fmt.Printf("   Log: %s\n", logFile)
		}
		fmt.Println()
	}

	fmt.Println(strings.Repeat("=", 65))
	fmt.Println("✨ - Next steps:")

	// Find services with HTTP ports and add direct links
	for _, service := range services {
		if httpPort, exists := service.Ports["http_port"]; exists {
			url := fmt.Sprintf("http://%s:%d", service.Fqdn, httpPort)
			fmt.Printf("- Open %s in your browser: %s\n", service.Name, url)
		}
	}

	fmt.Println("- Check cluster status: multigres cluster status")
	fmt.Println("- Stop cluster: multigres cluster stop")
	fmt.Println(strings.Repeat("=", 65))
}

var StartCommand = &cobra.Command{
	Use:   "start",
	Short: "Start local cluster",
	Long:  "Start a local Multigres cluster using the configuration created with 'multigres cluster init'.",
	RunE:  start,
}

func init() {
	StartCommand.Flags().String("admin-endpoint", "localhost:15990", "Admin server endpoint")
}
