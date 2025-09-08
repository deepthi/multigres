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

package command

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/multigres/multigres/go/cmd/multigres/internal/adminserver"
	adminpb "github.com/multigres/multigres/go/pb/adminservice"
	"github.com/multigres/multigres/go/servenv"

	"github.com/spf13/cobra"
)

// runAdminServer starts the admin gRPC server in background mode using servenv
func runAdminServer(cmd *cobra.Command, args []string) error {
	// Initialize servenv
	servenv.Init()

	// Get flags
	portStr, err := cmd.Flags().GetString("port")
	if err != nil {
		return fmt.Errorf("failed to get port flag: %w", err)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("invalid port %q: %w", portStr, err)
	}

	logFile, err := cmd.Flags().GetString("log-file")
	if err != nil {
		return fmt.Errorf("failed to get log-file flag: %w", err)
	}

	// Setup logging to file
	if logFile == "" {
		logFile = "multigres-admin.log"
	}

	// Make log file path absolute
	if !filepath.IsAbs(logFile) {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
		logFile = filepath.Join(cwd, logFile)
	}

	// Open log file
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file %s: %w", logFile, err)
	}

	// Setup structured logger to write to file
	logger := slog.New(slog.NewTextHandler(file, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	var server *adminserver.Server

	// Register what to do when service starts
	servenv.OnRun(func() {
		logger.Info("Admin server starting up", "port", port, "log_file", logFile)

		// Create and start server
		server = adminserver.NewServer()
		if err := server.Start(port); err != nil {
			logger.Error("Failed to start admin server", "error", err)
			os.Exit(1)
		}

		logger.Info("Admin server started", "address", server.Address())
		fmt.Printf("✅ Admin server listening on %s\n", server.Address())
		fmt.Printf("📄 Logs: %s\n", logFile)
		fmt.Printf("🛑 To stop: multigres admin stop --admin-endpoint %s\n", server.Address())
	})

	// Register cleanup when service shuts down
	servenv.OnClose(func() {
		logger.Info("Admin server shutting down")
		if server != nil {
			server.Stop()
		}
		file.Close()
		logger.Info("Admin server stopped")
	})

	// Start the service - this will block until shutdown signal
	servenv.RunDefault()

	return nil
}

// runAdminStop stops a running admin server
func runAdminStop(cmd *cobra.Command, args []string) error {
	endpoint, err := cmd.Flags().GetString("admin-endpoint")
	if err != nil {
		return fmt.Errorf("failed to get admin-endpoint flag: %w", err)
	}

	if endpoint == "" {
		return fmt.Errorf("admin-endpoint flag is required")
	}

	force, err := cmd.Flags().GetBool("force")
	if err != nil {
		return fmt.Errorf("failed to get force flag: %w", err)
	}

	fmt.Printf("🛑 Stopping admin server at %s...\n", endpoint)

	// Create gRPC client connection
	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to admin server at %s: %w", endpoint, err)
	}
	defer conn.Close()

	// Create admin client
	client := adminpb.NewAdminServiceClient(conn)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Call Shutdown RPC
	resp, err := client.Shutdown(ctx, &adminpb.ShutdownRequest{
		Force: force,
	})
	if err != nil {
		return fmt.Errorf("failed to shutdown admin server: %w", err)
	}

	fmt.Printf("✅ %s\n", resp.Message)

	return nil
}

var adminCmd = &cobra.Command{
	Use:   "admin",
	Short: "Manage admin server",
	Long:  "Commands for managing the Multigres admin server that provides gRPC API for cluster operations.",
}

var adminStartCmd = &cobra.Command{
	Use:     "start",
	Short:   "Start admin server",
	Long:    "Start the Multigres admin gRPC server that handles cluster operations in background mode.",
	PreRunE: servenv.CobraPreRunE,
	RunE:    runAdminServer,
}

var adminStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop admin server",
	Long:  "Stop a running Multigres admin gRPC server.",
	RunE:  runAdminStop,
}

func init() {
	// Register admin start command with servenv
	servenv.RegisterServiceCmd(adminStartCmd)

	// Add flags to admin start command
	adminStartCmd.Flags().String("port", "15990", "Port to listen on")
	adminStartCmd.Flags().String("log-file", "", "Log file path (default: multigres-admin.log in current directory)")

	// Add flags to admin stop command
	adminStopCmd.Flags().String("admin-endpoint", "", "Admin server endpoint (required)")
	adminStopCmd.Flags().Bool("force", false, "Force shutdown even if clusters are running")
	_ = adminStopCmd.MarkFlagRequired("admin-endpoint")

	// Register admin commands
	adminCmd.AddCommand(adminStartCmd)
	adminCmd.AddCommand(adminStopCmd)

	// Register admin command with root
	Root.AddCommand(adminCmd)
}
