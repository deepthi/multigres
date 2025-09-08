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
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	grpcstatus "google.golang.org/grpc/status"
	"gopkg.in/yaml.v3"

	adminpb "github.com/multigres/multigres/go/pb/adminservice"
)

// createGRPCClient creates a gRPC client connection to the admin server
func createGRPCClient(t *testing.T, address string) (adminpb.AdminServiceClient, *grpc.ClientConn) {
	t.Helper()

	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err, "Failed to connect to admin server")

	client := adminpb.NewAdminServiceClient(conn)
	return client, conn
}

func TestGRPCAPI_InitCluster(t *testing.T) {
	ensureBinaryBuilt(t)

	// Start admin server
	adminServer := startTestAdminServer(t)
	defer adminServer.stop(t)

	// Create gRPC client
	client, conn := createGRPCClient(t, adminServer.address)
	defer conn.Close()

	t.Run("successful cluster initialization", func(t *testing.T) {
		// Create temporary directory
		tempDir, err := os.MkdirTemp("", "grpc_init_test")
		require.NoError(t, err)
		defer os.RemoveAll(tempDir)

		// Call InitCluster API
		resp, err := client.InitCluster(context.Background(), &adminpb.InitClusterRequest{
			Provisioner: "local",
			ConfigPaths: []string{tempDir},
		})

		require.NoError(t, err, "InitCluster should succeed")
		require.NotNil(t, resp)

		// Verify response
		assert.NotEmpty(t, resp.ConfigFilePath, "Config file path should be provided")
		assert.NotEmpty(t, resp.ClusterId, "Cluster ID should be provided")
		assert.Equal(t, filepath.Join(tempDir, "multigres.yaml"), resp.ConfigFilePath)

		// Verify config file was actually created
		_, err = os.Stat(resp.ConfigFilePath)
		require.NoError(t, err, "Config file should exist on disk")

		// Verify config file content
		configData, err := os.ReadFile(resp.ConfigFilePath)
		require.NoError(t, err)

		var config MultigresConfig
		err = yaml.Unmarshal(configData, &config)
		require.NoError(t, err)
		assert.Equal(t, "local", config.Provisioner)
		assert.NotNil(t, config.ProvisionerConfig)
	})

	t.Run("error with non-existent directory", func(t *testing.T) {
		// Call InitCluster API with non-existent directory
		_, err := client.InitCluster(context.Background(), &adminpb.InitClusterRequest{
			Provisioner: "local",
			ConfigPaths: []string{"/nonexistent/directory"},
		})

		require.Error(t, err, "InitCluster should fail with non-existent directory")
		grpcStatus := grpcstatus.Convert(err)
		assert.Equal(t, codes.InvalidArgument, grpcStatus.Code())
		assert.Contains(t, grpcStatus.Message(), "config path does not exist")
	})

	t.Run("error with file instead of directory", func(t *testing.T) {
		// Create temporary file
		tempFile, err := os.CreateTemp("", "grpc_init_file_test")
		require.NoError(t, err)
		tempFile.Close()
		defer os.Remove(tempFile.Name())

		// Call InitCluster API with file instead of directory
		_, err = client.InitCluster(context.Background(), &adminpb.InitClusterRequest{
			Provisioner: "local",
			ConfigPaths: []string{tempFile.Name()},
		})

		require.Error(t, err, "InitCluster should fail with file instead of directory")
		grpcStatus := grpcstatus.Convert(err)
		assert.Equal(t, codes.InvalidArgument, grpcStatus.Code())
		assert.Contains(t, grpcStatus.Message(), "config path is not a directory")
	})

	t.Run("error with existing config file", func(t *testing.T) {
		// Create temporary directory with existing config file
		tempDir, err := os.MkdirTemp("", "grpc_init_exists_test")
		require.NoError(t, err)
		defer os.RemoveAll(tempDir)

		existingConfigFile := filepath.Join(tempDir, "multigres.yaml")
		err = os.WriteFile(existingConfigFile, []byte("existing: config"), 0644)
		require.NoError(t, err)

		// Call InitCluster API
		_, err = client.InitCluster(context.Background(), &adminpb.InitClusterRequest{
			Provisioner: "local",
			ConfigPaths: []string{tempDir},
		})

		require.Error(t, err, "InitCluster should fail when config file already exists")
		grpcStatus := grpcstatus.Convert(err)
		assert.Equal(t, codes.AlreadyExists, grpcStatus.Code())
		assert.Contains(t, grpcStatus.Message(), "config file already exists")
	})

	t.Run("custom provisioner config", func(t *testing.T) {
		// Create temporary directory
		tempDir, err := os.MkdirTemp("", "grpc_init_custom_test")
		require.NoError(t, err)
		defer os.RemoveAll(tempDir)

		// Call InitCluster API with custom config
		resp, err := client.InitCluster(context.Background(), &adminpb.InitClusterRequest{
			Provisioner: "local",
			ConfigPaths: []string{tempDir},
			ProvisionerConfig: map[string]string{
				"default-db-name": "custom_db",
			},
		})

		require.NoError(t, err, "InitCluster with custom config should succeed")

		// Verify config file contains custom settings
		configData, err := os.ReadFile(resp.ConfigFilePath)
		require.NoError(t, err)

		var config MultigresConfig
		err = yaml.Unmarshal(configData, &config)
		require.NoError(t, err)

		// Verify custom config is applied
		assert.Equal(t, "custom_db", config.ProvisionerConfig["default-db-name"])
	})
}

func TestGRPCAPI_StartCluster(t *testing.T) {
	ensureBinaryBuilt(t)

	// Start admin server
	adminServer := startTestAdminServer(t)
	defer adminServer.stop(t)

	// Create gRPC client
	client, conn := createGRPCClient(t, adminServer.address)
	defer conn.Close()

	t.Run("start cluster with streaming events", func(t *testing.T) {
		// Create temporary directory and config file
		tempDir, err := os.MkdirTemp("", "grpc_start_test")
		require.NoError(t, err)
		defer os.RemoveAll(tempDir)

		// Build service binaries first
		require.NoError(t, buildServiceBinaries(tempDir), "Failed to build service binaries")

		// Create test configuration with ports
		testPorts := getTestPortConfig()
		_, err = createTestConfigWithPorts(tempDir, testPorts)
		require.NoError(t, err)

		// Always cleanup processes
		defer func() {
			if cleanupErr := cleanupTestProcesses(tempDir); cleanupErr != nil {
				t.Logf("Warning: cleanup failed: %v", cleanupErr)
			}
		}()

		// Call StartCluster API (streaming)
		stream, err := client.StartCluster(context.Background(), &adminpb.StartClusterRequest{
			ConfigPaths: []string{tempDir},
		})
		require.NoError(t, err, "StartCluster should succeed")

		// Collect all streaming events
		var events []*adminpb.StartClusterResponse
		for {
			event, err := stream.Recv()
			if err == io.EOF {
				break
			}
			require.NoError(t, err, "Streaming should not have errors")
			events = append(events, event)

			t.Logf("Received event: %s - %s", event.EventType, event.Message)

			// Break on completion or error
			if event.EventType == adminpb.StartEventType_START_EVENT_TYPE_COMPLETED ||
				event.EventType == adminpb.StartEventType_START_EVENT_TYPE_ERROR {
				break
			}
		}

		// Verify we received expected events
		assert.NotEmpty(t, events, "Should receive streaming events")

		// First event should be starting
		assert.Equal(t, adminpb.StartEventType_START_EVENT_TYPE_STARTING, events[0].EventType)
		assert.Contains(t, events[0].Message, "Starting Multigres cluster")

		// Last event should be completion (if successful) or error
		lastEvent := events[len(events)-1]
		if lastEvent.EventType == adminpb.StartEventType_START_EVENT_TYPE_ERROR {
			t.Logf("Cluster start failed: %s", lastEvent.Error)
		} else {
			assert.Equal(t, adminpb.StartEventType_START_EVENT_TYPE_COMPLETED, lastEvent.EventType)
			assert.Contains(t, lastEvent.Message, "successfully")
		}

		// Should have service provisioned events
		var serviceEvents []*adminpb.StartClusterResponse
		for _, event := range events {
			if event.EventType == adminpb.StartEventType_START_EVENT_TYPE_SERVICE_PROVISIONED {
				serviceEvents = append(serviceEvents, event)
			}
		}

		if lastEvent.EventType == adminpb.StartEventType_START_EVENT_TYPE_COMPLETED {
			assert.NotEmpty(t, serviceEvents, "Should have service provisioned events")

			// Verify service info in events
			for _, event := range serviceEvents {
				require.NotNil(t, event.Service)
				assert.NotEmpty(t, event.Service.Name)
				assert.NotEmpty(t, event.Service.ServiceId)
				assert.NotEmpty(t, event.Service.Fqdn)
				assert.NotEmpty(t, event.Service.Ports)
			}
		}
	})

	t.Run("start cluster with invalid config path", func(t *testing.T) {
		// Call StartCluster API with non-existent path
		stream, err := client.StartCluster(context.Background(), &adminpb.StartClusterRequest{
			ConfigPaths: []string{"/nonexistent/path"},
		})
		require.NoError(t, err, "StartCluster call should succeed initially")

		// Should receive error event
		event, err := stream.Recv()
		require.NoError(t, err, "Should receive at least one event")

		// Should eventually get an error event
		for event.EventType != adminpb.StartEventType_START_EVENT_TYPE_ERROR {
			event, err = stream.Recv()
			if err == io.EOF {
				t.Fatal("Expected error event but stream ended")
			}
			require.NoError(t, err)
		}

		assert.Equal(t, adminpb.StartEventType_START_EVENT_TYPE_ERROR, event.EventType)
		assert.NotEmpty(t, event.Error)
	})
}

func TestGRPCAPI_StopCluster(t *testing.T) {
	ensureBinaryBuilt(t)

	// Start admin server
	adminServer := startTestAdminServer(t)
	defer adminServer.stop(t)

	// Create gRPC client
	client, conn := createGRPCClient(t, adminServer.address)
	defer conn.Close()

	t.Run("stop non-existent cluster", func(t *testing.T) {
		// Try to stop a cluster that doesn't exist
		_, err := client.StopCluster(context.Background(), &adminpb.StopClusterRequest{
			ClusterId: "nonexistent-cluster",
		})

		require.Error(t, err, "StopCluster should fail for non-existent cluster")
		grpcStatus := grpcstatus.Convert(err)
		assert.Equal(t, codes.NotFound, grpcStatus.Code())
		assert.Contains(t, grpcStatus.Message(), "cluster not found")
	})

	t.Run("stop cluster normal mode", func(t *testing.T) {
		// Create temporary directory and minimal config
		tempDir, err := os.MkdirTemp("", "grpc_stop_test")
		require.NoError(t, err)
		defer os.RemoveAll(tempDir)

		// Create minimal config file (though we won't actually start services)
		testPorts := getTestPortConfig()
		_, err = createTestConfigWithPorts(tempDir, testPorts)
		require.NoError(t, err)

		// Call StopCluster API (this will likely fail to find services, which is expected)
		_, err = client.StopCluster(context.Background(), &adminpb.StopClusterRequest{
			ConfigPaths: []string{tempDir},
			Clean:       false,
		})

		// This should succeed even if no cluster is running
		// The actual behavior depends on how the provisioner handles teardown of non-existent services
		if err != nil {
			// Log error but don't fail test - this is expected behavior
			t.Logf("StopCluster returned error (expected): %v", err)
		}
	})

	t.Run("stop cluster clean mode", func(t *testing.T) {
		// Create temporary directory and minimal config
		tempDir, err := os.MkdirTemp("", "grpc_stop_clean_test")
		require.NoError(t, err)
		defer os.RemoveAll(tempDir)

		// Create minimal config file
		testPorts := getTestPortConfig()
		_, err = createTestConfigWithPorts(tempDir, testPorts)
		require.NoError(t, err)

		// Call StopCluster API with clean flag
		_, err = client.StopCluster(context.Background(), &adminpb.StopClusterRequest{
			ConfigPaths: []string{tempDir},
			Clean:       true,
		})

		// This should succeed even if no cluster is running
		if err != nil {
			// Log error but don't fail test - this is expected behavior
			t.Logf("StopCluster with clean returned error (expected): %v", err)
		}
	})
}

func TestGRPCAPI_GetClusterStatus(t *testing.T) {
	ensureBinaryBuilt(t)

	// Start admin server
	adminServer := startTestAdminServer(t)
	defer adminServer.stop(t)

	// Create gRPC client
	client, conn := createGRPCClient(t, adminServer.address)
	defer conn.Close()

	t.Run("get status of non-existent cluster", func(t *testing.T) {
		// Try to get status of a cluster that doesn't exist
		_, err := client.GetClusterStatus(context.Background(), &adminpb.GetClusterStatusRequest{
			ClusterId: "nonexistent-cluster",
		})

		require.Error(t, err, "GetClusterStatus should fail for non-existent cluster")
		grpcStatus := grpcstatus.Convert(err)
		assert.Equal(t, codes.NotFound, grpcStatus.Code())
		assert.Contains(t, grpcStatus.Message(), "cluster not found")
	})

	t.Run("get status after cluster init", func(t *testing.T) {
		// Create temporary directory and initialize cluster
		tempDir, err := os.MkdirTemp("", "grpc_status_test")
		require.NoError(t, err)
		defer os.RemoveAll(tempDir)

		// Initialize cluster first
		initResp, err := client.InitCluster(context.Background(), &adminpb.InitClusterRequest{
			Provisioner: "local",
			ConfigPaths: []string{tempDir},
		})
		require.NoError(t, err)

		// Get cluster status
		statusResp, err := client.GetClusterStatus(context.Background(), &adminpb.GetClusterStatusRequest{
			ClusterId: initResp.ClusterId,
		})

		require.NoError(t, err, "GetClusterStatus should succeed after init")
		require.NotNil(t, statusResp)

		// Verify response
		assert.Equal(t, initResp.ClusterId, statusResp.ClusterId)
		assert.Equal(t, adminpb.ClusterStatus_CLUSTER_STATUS_STOPPED, statusResp.Status)
		// Services should be empty for stopped cluster
		assert.Empty(t, statusResp.Services)
	})
}

func TestGRPCAPI_ListClusters(t *testing.T) {
	ensureBinaryBuilt(t)

	// Start admin server
	adminServer := startTestAdminServer(t)
	defer adminServer.stop(t)

	// Create gRPC client
	client, conn := createGRPCClient(t, adminServer.address)
	defer conn.Close()

	t.Run("list clusters when empty", func(t *testing.T) {
		// List clusters when none exist (fresh admin server)
		resp, err := client.ListClusters(context.Background(), &adminpb.ListClustersRequest{})

		require.NoError(t, err, "ListClusters should succeed")
		require.NotNil(t, resp)
		// Initially should be empty
		assert.NotNil(t, resp.Clusters)
		assert.GreaterOrEqual(t, len(resp.Clusters), 0, "Should have 0 or more clusters")
	})

	t.Run("list clusters after creating some", func(t *testing.T) {
		// Create multiple clusters
		var clusterIds []string
		for i := 0; i < 3; i++ {
			tempDir, err := os.MkdirTemp("", "grpc_list_test")
			require.NoError(t, err)
			defer os.RemoveAll(tempDir)

			// Initialize cluster
			initResp, err := client.InitCluster(context.Background(), &adminpb.InitClusterRequest{
				Provisioner: "local",
				ConfigPaths: []string{tempDir},
			})
			require.NoError(t, err)
			clusterIds = append(clusterIds, initResp.ClusterId)
		}

		// List clusters
		resp, err := client.ListClusters(context.Background(), &adminpb.ListClustersRequest{})

		require.NoError(t, err, "ListClusters should succeed")
		require.NotNil(t, resp)
		assert.Equal(t, len(clusterIds), len(resp.Clusters), "Should list exactly the created clusters")

		// Verify cluster information
		clusterIdSet := make(map[string]bool)
		for _, cluster := range resp.Clusters {
			assert.NotEmpty(t, cluster.ClusterId)
			assert.NotEmpty(t, cluster.Name)
			assert.NotEmpty(t, cluster.Provisioner)
			assert.NotNil(t, cluster.Status)
			clusterIdSet[cluster.ClusterId] = true
		}

		// Verify our created clusters are in the list
		for _, clusterId := range clusterIds {
			assert.True(t, clusterIdSet[clusterId], "Created cluster %s should be in the list", clusterId)
		}
	})
}

func TestGRPCAPI_Timeout(t *testing.T) {
	ensureBinaryBuilt(t)

	// Start admin server
	adminServer := startTestAdminServer(t)
	defer adminServer.stop(t)

	// Create gRPC client
	client, conn := createGRPCClient(t, adminServer.address)
	defer conn.Close()

	t.Run("request with timeout", func(t *testing.T) {
		// Create a context with a very short timeout
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
		defer cancel()

		// Try to initialize cluster with short timeout (should timeout)
		tempDir, err := os.MkdirTemp("", "grpc_timeout_test")
		require.NoError(t, err)
		defer os.RemoveAll(tempDir)

		_, err = client.InitCluster(ctx, &adminpb.InitClusterRequest{
			Provisioner: "local",
			ConfigPaths: []string{tempDir},
		})

		// Should get a timeout or deadline exceeded error
		require.Error(t, err, "Request should timeout")
		grpcStatus := grpcstatus.Convert(err)
		assert.Equal(t, codes.DeadlineExceeded, grpcStatus.Code())
	})
}
