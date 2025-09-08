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

package adminserver

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gopkg.in/yaml.v3"

	adminpb "github.com/multigres/multigres/go/pb/adminservice"
	"github.com/multigres/multigres/go/provisioner"
)

// Server implements the AdminService gRPC server
type Server struct {
	adminpb.UnimplementedAdminServiceServer

	// grpcServer is the underlying gRPC server
	grpcServer *grpc.Server

	// listener is the network listener for the gRPC server
	listener net.Listener

	// provisioner is the provisioner instance used for cluster operations
	provisioner provisioner.Provisioner

	// clusterStates tracks the state of managed clusters
	clusterStates map[string]*ClusterState

	// mutex protects access to clusterStates
	mutex sync.RWMutex

	// shutdownCh is closed when the server should shut down
	shutdownCh chan struct{}
}

// ClusterState tracks the state of a cluster
type ClusterState struct {
	ID          string
	ConfigPath  string
	Status      adminpb.ClusterStatus
	Services    []*adminpb.ServiceInfo
	LastUpdated time.Time
	Error       string
}

// NewServer creates a new AdminService server
func NewServer() *Server {
	return &Server{
		clusterStates: make(map[string]*ClusterState),
		shutdownCh:    make(chan struct{}),
	}
}

// Start starts the gRPC server on the specified port
func (s *Server) Start(port int) error {
	// Create listener - bind to localhost (127.0.0.1) instead of all interfaces
	listener, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", port, err)
	}

	s.listener = listener

	// Create gRPC server
	s.grpcServer = grpc.NewServer()

	// Register our service
	adminpb.RegisterAdminServiceServer(s.grpcServer, s)

	// Start serving in a goroutine
	go func() {
		slog.Info("Admin server starting", "port", port)
		if err := s.grpcServer.Serve(listener); err != nil {
			slog.Error("Admin server failed", "error", err)
		}
	}()

	// Start shutdown monitor in a goroutine
	go func() {
		<-s.shutdownCh
		slog.Info("Shutdown signal received, stopping server")
		s.Stop()
	}()

	return nil
}

// Stop gracefully stops the gRPC server
func (s *Server) Stop() {
	// Close shutdown channel only if not already closed
	select {
	case <-s.shutdownCh:
		// Channel already closed, do nothing
	default:
		close(s.shutdownCh)
	}

	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
	}

	if s.listener != nil {
		s.listener.Close()
	}
}

// Address returns the address the server is listening on in IPv4 format when possible
func (s *Server) Address() string {
	if s.listener != nil {
		addr := s.listener.Addr()
		if tcpAddr, ok := addr.(*net.TCPAddr); ok {
			ip := tcpAddr.IP
			if ip.IsUnspecified() {
				// Unspecified (::) becomes localhost
				return fmt.Sprintf("localhost:%d", tcpAddr.Port)
			} else if ip.IsLoopback() {
				// Convert IPv6 loopback (::1) to IPv4 loopback (localhost)
				return fmt.Sprintf("localhost:%d", tcpAddr.Port)
			} else if ip.To4() != nil {
				// Already IPv4
				return fmt.Sprintf("%s:%d", ip.String(), tcpAddr.Port)
			} else {
				// IPv6 - retain the original IPv6 address as it's usable
				return addr.String()
			}
		}
		// Fallback to original string representation
		return addr.String()
	}
	return ""
}

// InitCluster implements the InitCluster RPC
func (s *Server) InitCluster(ctx context.Context, req *adminpb.InitClusterRequest) (*adminpb.InitClusterResponse, error) {
	slog.Info("InitCluster called", "provisioner", req.Provisioner, "config_paths", req.ConfigPaths)

	if req.Provisioner == "" {
		req.Provisioner = "local" // Default provisioner
	}

	// Validate config paths
	if len(req.ConfigPaths) == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "no config paths provided")
	}

	// Check that the first config path exists and is a directory
	configDir := req.ConfigPaths[0]
	if info, err := os.Stat(configDir); err != nil {
		if os.IsNotExist(err) {
			return nil, status.Errorf(codes.InvalidArgument, "config path does not exist: %s", configDir)
		}
		return nil, status.Errorf(codes.Internal, "failed to access config path %s: %v", configDir, err)
	} else if !info.IsDir() {
		return nil, status.Errorf(codes.InvalidArgument, "config path is not a directory: %s", configDir)
	}

	// Get provisioner
	prov, err := provisioner.GetProvisioner(req.Provisioner)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid provisioner %q: %v", req.Provisioner, err)
	}

	// Create default configuration
	defaultConfig := prov.DefaultConfig()

	// Apply any overrides from request
	for key, value := range req.ProvisionerConfig {
		defaultConfig[key] = value
	}

	// Validate configuration
	if err := prov.ValidateConfig(defaultConfig); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid configuration: %v", err)
	}

	// Create MultigresConfig structure
	multigresConfig := struct {
		Provisioner       string                 `yaml:"provisioner"`
		ProvisionerConfig map[string]interface{} `yaml:"provisioner-config,omitempty"`
	}{
		Provisioner:       req.Provisioner,
		ProvisionerConfig: defaultConfig,
	}

	// Marshal to YAML
	yamlData, err := yaml.Marshal(multigresConfig)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal config to YAML: %v", err)
	}

	// Determine config file path
	configPath := filepath.Join(configDir, "multigres.yaml")

	// Check if config file already exists
	if _, err := os.Stat(configPath); err == nil {
		return nil, status.Errorf(codes.AlreadyExists, "config file already exists: %s", configPath)
	}

	// Write config file
	if err := os.WriteFile(configPath, yamlData, 0644); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to write config file %s: %v", configPath, err)
	}

	slog.Info("Created configuration file", "path", configPath, "provisioner", req.Provisioner)

	// Generate cluster ID
	clusterID := fmt.Sprintf("cluster-%d", time.Now().Unix())

	// Track cluster state
	s.mutex.Lock()
	s.clusterStates[clusterID] = &ClusterState{
		ID:          clusterID,
		ConfigPath:  configPath,
		Status:      adminpb.ClusterStatus_CLUSTER_STATUS_STOPPED,
		Services:    []*adminpb.ServiceInfo{},
		LastUpdated: time.Now(),
	}
	s.mutex.Unlock()

	return &adminpb.InitClusterResponse{
		ConfigFilePath: configPath,
		ClusterId:      clusterID,
	}, nil
}

// StartCluster implements the StartCluster RPC
func (s *Server) StartCluster(req *adminpb.StartClusterRequest, stream adminpb.AdminService_StartClusterServer) error {
	slog.Info("StartCluster called", "config_paths", req.ConfigPaths, "cluster_id", req.ClusterId)

	// Load configuration from config paths
	// For now, we'll use the first config path or default to current directory
	configPaths := req.ConfigPaths
	if len(configPaths) == 0 {
		configPaths = []string{"."}
	}

	// Find or create cluster state
	var clusterState *ClusterState
	s.mutex.Lock()
	if req.ClusterId != "" {
		clusterState = s.clusterStates[req.ClusterId]
	} else {
		// Create a default cluster if none specified
		clusterID := "default"
		if existing := s.clusterStates[clusterID]; existing != nil {
			clusterState = existing
		} else {
			clusterState = &ClusterState{
				ID:         clusterID,
				ConfigPath: configPaths[0] + "/multigres.yaml",
				Status:     adminpb.ClusterStatus_CLUSTER_STATUS_STOPPED,
				Services:   []*adminpb.ServiceInfo{},
			}
			s.clusterStates[clusterID] = clusterState
		}
	}
	s.mutex.Unlock()

	if clusterState == nil {
		return status.Errorf(codes.NotFound, "cluster %q not found", req.ClusterId)
	}

	// Update cluster status to starting
	s.mutex.Lock()
	clusterState.Status = adminpb.ClusterStatus_CLUSTER_STATUS_STARTING
	clusterState.LastUpdated = time.Now()
	s.mutex.Unlock()

	// Send starting event
	if err := stream.Send(&adminpb.StartClusterResponse{
		EventType: adminpb.StartEventType_START_EVENT_TYPE_STARTING,
		Message:   "Starting Multigres cluster...",
	}); err != nil {
		return err
	}

	// Get and configure provisioner
	prov, err := s.getProvisionerForCluster(clusterState)
	if err != nil {
		s.updateClusterError(clusterState, err.Error())
		return stream.Send(&adminpb.StartClusterResponse{
			EventType: adminpb.StartEventType_START_EVENT_TYPE_ERROR,
			Error:     err.Error(),
		})
	}

	// Load provisioner configuration
	if err := prov.LoadConfig(configPaths); err != nil {
		s.updateClusterError(clusterState, fmt.Sprintf("failed to load config: %v", err))
		return stream.Send(&adminpb.StartClusterResponse{
			EventType: adminpb.StartEventType_START_EVENT_TYPE_ERROR,
			Error:     fmt.Sprintf("failed to load config: %v", err),
		})
	}

	// Bootstrap the cluster
	results, err := prov.Bootstrap(stream.Context())
	if err != nil {
		s.updateClusterError(clusterState, fmt.Sprintf("bootstrap failed: %v", err))
		return stream.Send(&adminpb.StartClusterResponse{
			EventType: adminpb.StartEventType_START_EVENT_TYPE_ERROR,
			Error:     fmt.Sprintf("bootstrap failed: %v", err),
		})
	}

	// Convert provisioner results to service info and send updates
	var services []*adminpb.ServiceInfo
	for _, result := range results {
		serviceInfo := &adminpb.ServiceInfo{
			Name:      result.ServiceName,
			ServiceId: fmt.Sprintf("%s-%d", result.ServiceName, time.Now().Unix()),
			Fqdn:      result.FQDN,
			Ports:     make(map[string]int32),
			Metadata:  make(map[string]string),
		}

		// Convert ports
		for name, port := range result.Ports {
			serviceInfo.Ports[name] = int32(port)
		}

		// Convert metadata
		for key, value := range result.Metadata {
			if strValue, ok := value.(string); ok {
				serviceInfo.Metadata[key] = strValue
			} else {
				serviceInfo.Metadata[key] = fmt.Sprintf("%v", value)
			}
		}

		services = append(services, serviceInfo)

		// Send service provisioned event
		if err := stream.Send(&adminpb.StartClusterResponse{
			EventType: adminpb.StartEventType_START_EVENT_TYPE_SERVICE_PROVISIONED,
			Message:   fmt.Sprintf("Service %s provisioned", result.ServiceName),
			Service:   serviceInfo,
		}); err != nil {
			return err
		}
	}

	// Update cluster state
	s.mutex.Lock()
	clusterState.Status = adminpb.ClusterStatus_CLUSTER_STATUS_RUNNING
	clusterState.Services = services
	clusterState.LastUpdated = time.Now()
	clusterState.Error = ""
	s.mutex.Unlock()

	// Send completion event
	return stream.Send(&adminpb.StartClusterResponse{
		EventType: adminpb.StartEventType_START_EVENT_TYPE_COMPLETED,
		Message:   "Cluster started successfully!",
	})
}

// StopCluster implements the StopCluster RPC
func (s *Server) StopCluster(ctx context.Context, req *adminpb.StopClusterRequest) (*adminpb.StopClusterResponse, error) {
	slog.Info("StopCluster called", "cluster_id", req.ClusterId, "clean", req.Clean)

	// Find cluster state
	s.mutex.RLock()
	var clusterState *ClusterState
	if req.ClusterId != "" {
		clusterState = s.clusterStates[req.ClusterId]
	} else {
		// Stop default cluster if none specified
		clusterState = s.clusterStates["default"]
	}
	s.mutex.RUnlock()

	if clusterState == nil {
		return nil, status.Errorf(codes.NotFound, "cluster not found")
	}

	// Update status to stopping
	s.mutex.Lock()
	clusterState.Status = adminpb.ClusterStatus_CLUSTER_STATUS_STOPPING
	clusterState.LastUpdated = time.Now()
	s.mutex.Unlock()

	// Get provisioner
	prov, err := s.getProvisionerForCluster(clusterState)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get provisioner: %v", err)
	}

	// Load config
	configPaths := req.ConfigPaths
	if len(configPaths) == 0 {
		configPaths = []string{"."}
	}

	if err := prov.LoadConfig(configPaths); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to load config: %v", err)
	}

	// Teardown the cluster
	if err := prov.Teardown(ctx, req.Clean); err != nil {
		s.updateClusterError(clusterState, fmt.Sprintf("teardown failed: %v", err))
		return nil, status.Errorf(codes.Internal, "teardown failed: %v", err)
	}

	// Update cluster state
	s.mutex.Lock()
	clusterState.Status = adminpb.ClusterStatus_CLUSTER_STATUS_STOPPED
	clusterState.Services = []*adminpb.ServiceInfo{}
	clusterState.LastUpdated = time.Now()
	clusterState.Error = ""
	s.mutex.Unlock()

	return &adminpb.StopClusterResponse{
		Message:         "Cluster stopped successfully",
		ServicesStopped: []string{}, // TODO: Track stopped services
	}, nil
}

// GetClusterStatus implements the GetClusterStatus RPC
func (s *Server) GetClusterStatus(ctx context.Context, req *adminpb.GetClusterStatusRequest) (*adminpb.GetClusterStatusResponse, error) {
	slog.Debug("GetClusterStatus called", "cluster_id", req.ClusterId)

	// Find cluster state
	s.mutex.RLock()
	var clusterState *ClusterState
	if req.ClusterId != "" {
		clusterState = s.clusterStates[req.ClusterId]
	} else {
		// Get default cluster if none specified
		clusterState = s.clusterStates["default"]
	}
	s.mutex.RUnlock()

	if clusterState == nil {
		return nil, status.Errorf(codes.NotFound, "cluster not found")
	}

	// Create service status from service info
	var serviceStatuses []*adminpb.ServiceStatus
	for _, service := range clusterState.Services {
		serviceStatuses = append(serviceStatuses, &adminpb.ServiceStatus{
			ServiceInfo:   service,
			Status:        adminpb.ServiceState_SERVICE_STATE_RUNNING, // Assume running for now
			Health:        adminpb.HealthStatus_HEALTH_STATUS_HEALTHY, // Assume healthy for now
			LastCheckTime: clusterState.LastUpdated.Unix(),
			ErrorMessage:  clusterState.Error,
		})
	}

	return &adminpb.GetClusterStatusResponse{
		ClusterId: clusterState.ID,
		Status:    clusterState.Status,
		Services:  serviceStatuses,
		Message:   clusterState.Error,
	}, nil
}

// ListClusters implements the ListClusters RPC
func (s *Server) ListClusters(ctx context.Context, req *adminpb.ListClustersRequest) (*adminpb.ListClustersResponse, error) {
	slog.Debug("ListClusters called")

	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var clusters []*adminpb.ClusterInfo
	for _, state := range s.clusterStates {
		clusters = append(clusters, &adminpb.ClusterInfo{
			ClusterId:      state.ID,
			Name:           state.ID, // Use ID as name for now
			Provisioner:    "local",  // TODO: Track provisioner type
			Status:         state.Status,
			ConfigFilePath: state.ConfigPath,
		})
	}

	return &adminpb.ListClustersResponse{
		Clusters: clusters,
	}, nil
}

// Shutdown implements the Shutdown RPC
func (s *Server) Shutdown(ctx context.Context, req *adminpb.ShutdownRequest) (*adminpb.ShutdownResponse, error) {
	slog.Info("Shutdown called", "force", req.Force)

	// Check if any clusters are running
	s.mutex.RLock()
	runningClusters := 0
	for _, state := range s.clusterStates {
		if state.Status == adminpb.ClusterStatus_CLUSTER_STATUS_RUNNING ||
			state.Status == adminpb.ClusterStatus_CLUSTER_STATUS_STARTING {
			runningClusters++
		}
	}
	s.mutex.RUnlock()

	if runningClusters > 0 && !req.Force {
		return nil, status.Errorf(codes.FailedPrecondition,
			"cannot shutdown admin server: %d cluster(s) are still running. Use force=true to override",
			runningClusters)
	}

	// Log shutdown
	if runningClusters > 0 {
		slog.Warn("Force shutdown requested with running clusters", "running_clusters", runningClusters)
	}
	slog.Info("Admin server shutting down")

	// Trigger shutdown in a goroutine to allow the RPC response to be sent
	go func() {
		// Give the RPC response time to be sent
		time.Sleep(100 * time.Millisecond)
		close(s.shutdownCh)
	}()

	return &adminpb.ShutdownResponse{
		Message: "Admin server shutting down",
	}, nil
}

// Helper methods

func (s *Server) getProvisionerForCluster(cluster *ClusterState) (provisioner.Provisioner, error) {
	// For now, always use local provisioner
	// TODO: Determine provisioner type from cluster configuration
	return provisioner.GetProvisioner("local")
}

func (s *Server) updateClusterError(cluster *ClusterState, errorMsg string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	cluster.Status = adminpb.ClusterStatus_CLUSTER_STATUS_ERROR
	cluster.Error = errorMsg
	cluster.LastUpdated = time.Now()
}
