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

package adminclient

import (
	"context"
	"fmt"
	"io"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	adminpb "github.com/multigres/multigres/go/pb/adminservice"
)

// Client provides a convenient interface for calling the AdminService
type Client struct {
	conn     *grpc.ClientConn
	client   adminpb.AdminServiceClient
	endpoint string
}

// NewClient creates a new admin client connected to the specified endpoint
func NewClient(endpoint string) (*Client, error) {
	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to admin server at %s: %w", endpoint, err)
	}

	client := adminpb.NewAdminServiceClient(conn)

	return &Client{
		conn:     conn,
		client:   client,
		endpoint: endpoint,
	}, nil
}

// Close closes the connection to the admin server
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// InitCluster initializes a new cluster configuration
func (c *Client) InitCluster(ctx context.Context, configPaths []string, provisioner string) (*adminpb.InitClusterResponse, error) {
	req := &adminpb.InitClusterRequest{
		ConfigPaths: configPaths,
		Provisioner: provisioner,
	}

	return c.client.InitCluster(ctx, req)
}

// StartClusterFunc is a function type for handling cluster start events
type StartClusterFunc func(*adminpb.StartClusterResponse) error

// StartCluster starts a cluster and streams events to the provided callback
func (c *Client) StartCluster(ctx context.Context, configPaths []string, clusterID string, callback StartClusterFunc) error {
	req := &adminpb.StartClusterRequest{
		ConfigPaths: configPaths,
		ClusterId:   clusterID,
	}

	stream, err := c.client.StartCluster(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to start cluster stream: %w", err)
	}

	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("stream error: %w", err)
		}

		// Call the callback with the event
		if callback != nil {
			if err := callback(resp); err != nil {
				return fmt.Errorf("callback error: %w", err)
			}
		}

		// Check if we're done
		if resp.EventType == adminpb.StartEventType_START_EVENT_TYPE_COMPLETED ||
			resp.EventType == adminpb.StartEventType_START_EVENT_TYPE_ERROR {
			return nil
		}
	}
}

// StopCluster stops a cluster
func (c *Client) StopCluster(ctx context.Context, configPaths []string, clusterID string, clean bool) (*adminpb.StopClusterResponse, error) {
	req := &adminpb.StopClusterRequest{
		ConfigPaths: configPaths,
		ClusterId:   clusterID,
		Clean:       clean,
	}

	return c.client.StopCluster(ctx, req)
}

// GetClusterStatus gets the status of a cluster
func (c *Client) GetClusterStatus(ctx context.Context, configPaths []string, clusterID string) (*adminpb.GetClusterStatusResponse, error) {
	req := &adminpb.GetClusterStatusRequest{
		ConfigPaths: configPaths,
		ClusterId:   clusterID,
	}

	return c.client.GetClusterStatus(ctx, req)
}

// ListClusters lists all known clusters
func (c *Client) ListClusters(ctx context.Context, configPaths []string) (*adminpb.ListClustersResponse, error) {
	req := &adminpb.ListClustersRequest{
		ConfigPaths: configPaths,
	}

	return c.client.ListClusters(ctx, req)
}
