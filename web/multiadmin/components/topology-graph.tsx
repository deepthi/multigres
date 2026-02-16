"use client";

import React, {
  useEffect,
  useState,
  useMemo,
  useCallback,
  useRef,
} from "react";
import "./topology-graph.css";
import ReactFlow, {
  Background,
  BackgroundVariant,
  type Node,
  type Edge,
} from "reactflow";
import "reactflow/dist/style.css";
import { cn } from "@/lib/utils";
import { useApi, ApiError } from "@/lib/api";
import type {
  MultiGateway,
  MultiPoolerWithStatus,
  MultiOrch,
  GracePeriod,
  ID,
} from "@/lib/api";
import { Loader2 } from "lucide-react";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";

// Utility functions for smart data comparison
function getStableId(id?: ID): string {
  return id ? `${id.component}:${id.cell}:${id.name}` : "";
}

// Helper function to format WAL position (LSN)
function formatWalPosition(pooler: MultiPoolerWithStatus): string {
  if (!pooler.status) return "N/A";

  // For primary: show current LSN from primary_status
  if (pooler.type === "PRIMARY" && pooler.status.primary_status?.lsn) {
    return pooler.status.primary_status.lsn;
  }

  // For replica: show last replay LSN from replication_status
  if (
    pooler.type === "REPLICA" &&
    pooler.status.replication_status?.last_replay_lsn
  ) {
    return pooler.status.replication_status.last_replay_lsn;
  }

  // Fallback to wal_position if available
  if (pooler.status.wal_position) {
    return pooler.status.wal_position;
  }

  return "N/A";
}

// Helper function to check if a replica is connected to the primary
function isReplicaConnected(
  replica: MultiPoolerWithStatus,
  primary: MultiPoolerWithStatus,
): boolean {
  if (!primary.status?.primary_status?.connected_followers || !replica.id) {
    return false;
  }

  return primary.status.primary_status.connected_followers.some(
    (follower) =>
      follower.cell === replica.id?.cell && follower.name === replica.id?.name,
  );
}

// Helper function to count connected replicas for a primary
function countConnectedReplicas(
  primary: MultiPoolerWithStatus,
  replicas: MultiPoolerWithStatus[],
): number {
  if (!primary.status?.primary_status?.connected_followers) {
    return 0;
  }

  return replicas.filter((replica) => isReplicaConnected(replica, primary))
    .length;
}

// Helper function to check if primary has valid status
function hasPrimaryStatus(primary: MultiPoolerWithStatus): boolean {
  // Check if we have status at all (gRPC call succeeded)
  if (!primary.status) {
    return false;
  }

  // Check if postgres is running and we have primary status
  if (!primary.status.postgres_running || !primary.status.primary_status) {
    return false;
  }

  return true;
}

// Helper function to calculate lag from timestamp
function calculateLagFromTimestamp(timestamp: string): string {
  try {
    const replayTime = new Date(timestamp);
    const now = new Date();
    const lagMs = now.getTime() - replayTime.getTime();

    // Show in milliseconds if < 1000ms, otherwise show in seconds
    if (lagMs < 1000) {
      return `${lagMs}ms`;
    } else {
      return `${(lagMs / 1000).toFixed(1)}s`;
    }
  } catch (err) {
    console.error("Failed to parse timestamp:", err);
    return "N/A";
  }
}

// Helper function to format replication lag
function formatReplicationLag(pooler: MultiPoolerWithStatus): string {
  if (!pooler.status) return "N/A";

  // Only show lag for replicas
  if (
    pooler.type === "REPLICA" &&
    pooler.status.replication_status?.last_xact_replay_timestamp
  ) {
    return calculateLagFromTimestamp(
      pooler.status.replication_status.last_xact_replay_timestamp,
    );
  }

  return "N/A";
}

function hasDataChanged<T extends { id?: ID }>(prev: T[], next: T[]): boolean {
  // Check topology structure change (nodes added/removed)
  if (prev.length !== next.length) return true;

  const prevIds = new Set(prev.map((item) => getStableId(item.id)));
  const nextIds = new Set(next.map((item) => getStableId(item.id)));

  if (prevIds.size !== nextIds.size) return true;
  for (const id of nextIds) {
    if (!prevIds.has(id)) return true;
  }

  // Check field changes
  const prevMap = new Map(prev.map((item) => [getStableId(item.id), item]));
  for (const item of next) {
    const id = getStableId(item.id);
    const prevItem = prevMap.get(id);
    if (!prevItem) continue;
    if (JSON.stringify(prevItem) !== JSON.stringify(item)) {
      return true;
    }
  }

  return false;
}

function StatusBadge({ status }: { status: string }) {
  const isActive = status.toLowerCase() === "active";
  return (
    <div className={cn(isActive ? "text-green-500" : "text-red-500")}>
      {status}
    </div>
  );
}

function PoolerTypeBadge({ type }: { type: "primary" | "replica" }) {
  return (
    <span
      className={cn(
        "text-xs px-1.5 py-0.5 rounded",
        type === "primary"
          ? "bg-blue-500/20 text-blue-400"
          : "bg-purple-500/20 text-purple-400",
      )}
    >
      {type}
    </span>
  );
}

function isPrimary(pooler: MultiPoolerWithStatus): boolean {
  return pooler.type === "PRIMARY";
}

// Countdown timer driven purely client-side after mount.
// Locks the deadline on first render so backend polling doesn't cause jumps.
function CountdownTimer({ deadline }: { deadline: string }) {
  const targetRef = useRef(new Date(deadline).getTime());
  const initialRemainingRef = useRef(
    Math.max(1, targetRef.current - Date.now()),
  );
  const [remaining, setRemaining] = useState(
    () => initialRemainingRef.current,
  );
  const [appointing, setAppointing] = useState(false);

  useEffect(() => {
    const interval = setInterval(() => {
      const ms = Math.max(0, targetRef.current - Date.now());
      setRemaining(ms);
      if (ms <= 0) {
        setAppointing(true);
      }
    }, 100);
    return () => clearInterval(interval);
  }, []);

  if (appointing) {
    return (
      <div className="text-red-400 font-bold animate-pulse">Appointing...</div>
    );
  }

  const seconds = (remaining / 1000).toFixed(1);
  const progress = remaining / initialRemainingRef.current;

  return (
    <div className="mt-1">
      <div className="text-amber-400 font-mono text-sm font-bold">
        Appointment Timeout {seconds}s
      </div>
      <div className="mt-1 h-1 w-full bg-muted rounded-full overflow-hidden">
        <div
          className="h-full bg-amber-500 rounded-full"
          style={{ width: `${progress * 100}%` }}
        />
      </div>
    </div>
  );
}

export function TopologyGraph({
  heightClass: _heightClass,
}: {
  heightClass?: string;
}) {
  const api = useApi();
  const [gateways, setGateways] = useState<MultiGateway[]>([]);
  const [poolers, setPoolers] = useState<MultiPoolerWithStatus[]>([]);
  const [orchs, setOrchs] = useState<MultiOrch[]>([]);
  const [orchGracePeriods, setOrchGracePeriods] = useState<
    Map<string, GracePeriod[]>
  >(new Map());
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Context menu state
  const [contextMenu, setContextMenu] = useState<{
    x: number;
    y: number;
    pooler: MultiPoolerWithStatus;
  } | null>(null);

  // Shutdown confirmation dialog state
  const [revokeTarget, setRevokeTarget] = useState<MultiPoolerWithStatus | null>(null);
  const [revoking, setRevoking] = useState(false);
  const [revokeError, setRevokeError] = useState<string | null>(null);

  const prevDataRef = useRef({
    gateways: [] as MultiGateway[],
    poolers: [] as MultiPoolerWithStatus[],
    orchs: [] as MultiOrch[],
  });

  const fetchData = useCallback(
    async (isInitial = false) => {
      try {
        if (isInitial) {
          setLoading(true);
        }
        setError(null);

        const [gatewaysRes, poolersRes, orchsRes] = await Promise.all([
          api.getGateways(),
          api.getPoolers(),
          api.getOrchs(),
        ]);

        const newGateways = gatewaysRes.gateways || [];
        const basePoolers = poolersRes.poolers || [];
        const newOrchs = orchsRes.orchs || [];

        // Fetch status for each pooler in parallel
        const poolerStatusPromises = basePoolers.map(async (pooler) => {
          if (!pooler.id) return pooler;

          try {
            const statusRes = await api.getPoolerStatus(pooler.id);
            return { ...pooler, status: statusRes.status };
          } catch (err) {
            console.error(
              `Failed to fetch status for pooler ${pooler.id.name}:`,
              err,
            );
            return pooler; // Return pooler without status if fetch fails
          }
        });

        const newPoolers = await Promise.all(poolerStatusPromises);

        // Fetch grace periods for each orch in parallel
        const gracePeriodMap = new Map<string, GracePeriod[]>();
        const gracePromises = newOrchs.map(async (orch) => {
          if (!orch.id) return;
          try {
            const res = await api.getOrchGracePeriods(orch.id);
            if (res.grace_periods?.length > 0) {
              gracePeriodMap.set(orch.id.name, res.grace_periods);
            }
          } catch {
            // Silently ignore - orch may not support this endpoint yet
          }
        });
        await Promise.all(gracePromises);
        setOrchGracePeriods(gracePeriodMap);

        // Smart comparison - only update if data actually changed
        const gatewaysChanged = hasDataChanged(
          prevDataRef.current.gateways,
          newGateways,
        );
        const poolersChanged = hasDataChanged(
          prevDataRef.current.poolers,
          newPoolers,
        );
        const orchsChanged = hasDataChanged(
          prevDataRef.current.orchs,
          newOrchs,
        );

        if (gatewaysChanged || poolersChanged || orchsChanged) {
          setGateways(newGateways);
          setPoolers(newPoolers);
          setOrchs(newOrchs);
          prevDataRef.current = {
            gateways: newGateways,
            poolers: newPoolers,
            orchs: newOrchs,
          };
        }
      } catch (err) {
        // On initial fetch, show error
        if (isInitial) {
          setError(
            err instanceof Error ? err.message : "Failed to fetch topology",
          );
        } else {
          // On polling errors, log but don't disrupt UI
          console.error("Polling error:", err);
        }
      } finally {
        if (isInitial) {
          setLoading(false);
        }
      }
    },
    [api],
  );

  useEffect(() => {
    let cancelled = false;

    // Get poll interval from env or use default (500ms)
    const pollInterval = parseInt(
      process.env.NEXT_PUBLIC_TOPOLOGY_POLL_INTERVAL || "500",
      10,
    );

    // Sequential polling: wait for fetch to complete, then wait interval
    async function poll() {
      await fetchData(true);
      while (!cancelled) {
        await new Promise((resolve) => setTimeout(resolve, pollInterval));
        if (cancelled) break;
        await fetchData(false);
      }
    }

    poll();

    return () => {
      cancelled = true;
    };
  }, [fetchData]);

  const { nodes, edges, poolerNodeMap } = useMemo(() => {
    const poolerNodeMap = new Map<string, MultiPoolerWithStatus>();

    if (gateways.length === 0 && poolers.length === 0 && orchs.length === 0) {
      return { nodes: [], edges: [], poolerNodeMap };
    }

    const nodes: Node[] = [];
    const edges: Edge[] = [];

    // Layout constants
    const GATEWAY_Y = 0;
    const PRIMARY_Y = 200;
    const REPLICA_Y = 450;
    const NODE_WIDTH = 150;
    const NODE_GAP = 100;
    const ORCH_NODE_HEIGHT = 120;
    const ORCH_NODE_GAP = 60;

    // Create gateway nodes
    const gatewayStartX = 100;
    gateways.forEach((gw, index) => {
      const id = `gw-${gw.id?.name || index}`;
      nodes.push({
        id,
        position: {
          x: gatewayStartX + index * (NODE_WIDTH + NODE_GAP),
          y: GATEWAY_Y,
        },
        data: {
          label: (
            <div className="text-left text-xs">
              <div className="bg-sidebar p-3 border-b">
                <div className="text-muted-foreground text-[10px] uppercase tracking-wide mb-1">
                  Gateway
                </div>
                <div className="font-semibold">
                  {gw.id?.name || `gateway-${index + 1}`}
                </div>
                <div className="mt-1">
                  <StatusBadge status="Active" />
                </div>
              </div>
              <div className="grid grid-cols-2 gap-x-4 gap-y-1 p-3">
                <div className="text-muted-foreground">Cell</div>
                <div className="text-right">{gw.id?.cell || "unknown"}</div>
              </div>
            </div>
          ),
        },
      });
    });

    // Group poolers by database/table_group/shard
    const poolerGroups = new Map<
      string,
      { primaries: MultiPoolerWithStatus[]; replicas: MultiPoolerWithStatus[] }
    >();

    poolers.forEach((pooler) => {
      const key = `${pooler.database || ""}/${pooler.table_group || "default"}/${pooler.shard || "0-"}`;
      if (!poolerGroups.has(key)) {
        poolerGroups.set(key, { primaries: [], replicas: [] });
      }
      const group = poolerGroups.get(key)!;
      if (isPrimary(pooler)) {
        group.primaries.push(pooler);
      } else {
        group.replicas.push(pooler);
      }
    });

    // Compute the center X of the gateway row for alignment
    const gatewayCenterX =
      gateways.length > 0
        ? gatewayStartX +
          ((gateways.length - 1) * (NODE_WIDTH + NODE_GAP)) / 2
        : gatewayStartX;

    // Create pooler nodes for each group
    let groupIndex = 0;
    poolerGroups.forEach((group, key) => {
      const [database, tableGroup, shard] = key.split("/");

      // Primary nodes - render all primaries
      group.primaries.forEach((primary, primaryIndex) => {
        const primaryId = `pooler-${primary.id?.name || `primary-${groupIndex}-${primaryIndex}`}`;
        poolerNodeMap.set(primaryId, primary);
        const hasValidStatus = hasPrimaryStatus(primary);
        const hasConnectedReplicas =
          countConnectedReplicas(primary, group.replicas) > 0;

        // Show red border if: no valid status OR (has replicas but none connected)
        const showDisconnected =
          !hasValidStatus ||
          (!hasConnectedReplicas && group.replicas.length > 0);

        // Center primary under the gateway row
        const primaryX =
          gatewayCenterX + primaryIndex * (NODE_WIDTH + NODE_GAP);

        nodes.push({
          id: primaryId,
          position: { x: primaryX, y: PRIMARY_Y },
          className: showDisconnected ? "disconnected-node" : "",
          data: {
            label: (
              <div className="text-left text-xs">
                <div className="bg-sidebar p-3 border-b">
                  <div className="text-muted-foreground text-[10px] uppercase tracking-wide mb-1 flex items-center gap-2">
                    Pooler <PoolerTypeBadge type="primary" />
                  </div>
                  <div className="font-semibold">
                    {primary.id?.name || "primary"}
                  </div>
                  <div className="text-muted-foreground">
                    {database} / {tableGroup} / {shard}
                  </div>
                </div>
                <div className="grid grid-cols-2 gap-x-4 gap-y-1 p-3">
                  <div className="text-muted-foreground">Cell</div>
                  <div className="text-right">
                    {primary.id?.cell || "unknown"}
                  </div>
                  <div className="text-muted-foreground">WAL Pos</div>
                  <div className="text-right text-xs">
                    {formatWalPosition(primary)}
                  </div>
                  <div className="text-muted-foreground">Lag</div>
                  <div className="text-right">
                    {formatReplicationLag(primary)}
                  </div>
                </div>
              </div>
            ),
          },
        });

        // Connect all gateways to this primary (only if it has valid status)
        if (hasValidStatus) {
          gateways.forEach((gw, gwIndex) => {
            const gwId = `gw-${gw.id?.name || gwIndex}`;
            edges.push({
              id: `${gwId}-${primaryId}`,
              source: gwId,
              target: primaryId,
            });
          });
        }
      });

      // Replica nodes - centered under the gateway row
      const replicaTotalWidth =
        group.replicas.length * NODE_WIDTH +
        (group.replicas.length - 1) * NODE_GAP;
      const replicaStartX = gatewayCenterX - replicaTotalWidth / 2 + NODE_WIDTH / 2;

      group.replicas.forEach((replica, replicaIndex) => {
        const replicaId = `pooler-${replica.id?.name || `replica-${groupIndex}-${replicaIndex}`}`;
        poolerNodeMap.set(replicaId, replica);
        const replicaX = replicaStartX + replicaIndex * (NODE_WIDTH + NODE_GAP);

        // Find the primary this replica is connected to (if any)
        const connectedPrimary = group.primaries.find((p) =>
          isReplicaConnected(replica, p),
        );
        const isConnected = !!connectedPrimary;

        nodes.push({
          id: replicaId,
          position: { x: replicaX, y: REPLICA_Y },
          className: !isConnected ? "disconnected-node" : "",
          data: {
            label: (
              <div className="text-left text-xs">
                <div className="bg-sidebar p-3 border-b">
                  <div className="text-muted-foreground text-[10px] uppercase tracking-wide mb-1 flex items-center gap-2">
                    Pooler <PoolerTypeBadge type="replica" />
                  </div>
                  <div className="font-semibold">
                    {replica.id?.name || `replica-${replicaIndex + 1}`}
                  </div>
                  <div className="text-muted-foreground">
                    {database} / {tableGroup} / {shard}
                  </div>
                </div>
                <div className="grid grid-cols-2 gap-x-4 gap-y-1 p-3">
                  <div className="text-muted-foreground">Cell</div>
                  <div className="text-right">
                    {replica.id?.cell || "unknown"}
                  </div>
                  <div className="text-muted-foreground">WAL Pos</div>
                  <div className="text-right text-xs">
                    {formatWalPosition(replica)}
                  </div>
                  <div className="text-muted-foreground">Lag</div>
                  <div className="text-right">
                    {formatReplicationLag(replica)}
                  </div>
                </div>
              </div>
            ),
          },
        });

        // Connect primary to replica (only if connected)
        if (isConnected && connectedPrimary) {
          const connectedPrimaryId = `pooler-${connectedPrimary.id?.name || `primary-${groupIndex}-${group.primaries.indexOf(connectedPrimary)}`}`;
          edges.push({
            id: `${connectedPrimaryId}-${replicaId}`,
            source: connectedPrimaryId,
            target: replicaId,
            className: "connected-edge",
            animated: true,
          });
        }
      });

      groupIndex++;
    });

    // Detect cluster health: unhealthy if any pooler node is disconnected
    const clusterUnhealthy = nodes.some(
      (node) => node.className === "disconnected-node",
    );

    // Check if a healthy primary exists — if so, appointment already happened
    // and countdowns on other orchs are stale.
    const hasHealthyPrimary = poolers.some(
      (p) => isPrimary(p) && hasPrimaryStatus(p),
    );

    // Create orchestrator nodes - stacked vertically to the right
    if (orchs.length > 0) {
      // Find the rightmost x position of existing nodes
      const maxX = nodes.reduce(
        (max, node) => Math.max(max, node.position.x),
        0,
      );
      const orchX = maxX + NODE_WIDTH + NODE_GAP / 2;

      // Center the stack vertically, shifted down
      const totalOrchHeight =
        orchs.length * ORCH_NODE_HEIGHT +
        (orchs.length - 1) * ORCH_NODE_GAP;
      const orchStartY = REPLICA_Y - totalOrchHeight / 2;

      orchs.forEach((orch, index) => {
        const id = `orch-${orch.id?.name || index}`;
        const orchName = orch.id?.name || `orch-${index + 1}`;
        const gracePeriods = orchGracePeriods.get(orchName) || [];
        const actingEntry = gracePeriods.find((gp) => gp.acting);
        const countdownEntry = gracePeriods.find((gp) => !gp.acting && gp.deadline);
        const showGracePeriod =
          gracePeriods.length > 0 &&
          (!!actingEntry || !hasHealthyPrimary);
        const orchClassName = showGracePeriod
          ? "election-pending-node"
          : clusterUnhealthy
            ? "recovering-node"
            : "monitoring-node";

        // Determine what to show on this orch node.
        // If a healthy primary exists, the appointment already happened —
        // suppress countdowns from orchs with stale grace periods.
        let orchContent: React.ReactNode;
        if (actingEntry) {
          // This orch fired the appointment action
          orchContent = (
            <div className="text-red-400 font-bold animate-pulse">
              Appointing...
            </div>
          );
        } else if (countdownEntry && !hasHealthyPrimary) {
          // This orch is counting down to appointment (no primary yet)
          orchContent = (
            <CountdownTimer deadline={countdownEntry.deadline} />
          );
        } else {
          const orchStatus = clusterUnhealthy ? "Recovering" : "Monitoring";
          orchContent = (
            <div
              className={cn(
                clusterUnhealthy ? "text-amber-500" : "text-green-500",
              )}
            >
              {orchStatus}
            </div>
          );
        }

        nodes.push({
          id,
          position: {
            x: orchX,
            y: orchStartY + index * (ORCH_NODE_HEIGHT + ORCH_NODE_GAP),
          },
          className: orchClassName,
          data: {
            label: (
              <div className="text-left text-xs">
                <div className="bg-sidebar p-3 border-b">
                  <div className="text-muted-foreground text-[10px] uppercase tracking-wide mb-1">
                    Orchestrator
                  </div>
                  <div className="font-semibold">{orchName}</div>
                  <div className="mt-1">{orchContent}</div>
                </div>
                <div className="grid grid-cols-2 gap-x-4 gap-y-1 p-3">
                  <div className="text-muted-foreground">Cell</div>
                  <div className="text-right">
                    {orch.id?.cell || "unknown"}
                  </div>
                </div>
              </div>
            ),
          },
        });
      });
    }

    return { nodes, edges, poolerNodeMap };
  }, [gateways, poolers, orchs, orchGracePeriods]);

  // Context menu: right-click on primary nodes
  const handleNodeContextMenu = useCallback(
    (event: React.MouseEvent, node: Node) => {
      event.preventDefault();
      const pooler = poolerNodeMap.get(node.id);
      if (pooler && pooler.type === "PRIMARY") {
        setContextMenu({ x: event.clientX, y: event.clientY, pooler });
      }
    },
    [poolerNodeMap],
  );

  const handlePaneClick = useCallback(() => {
    setContextMenu(null);
  }, []);

  // Close context menu on Escape or scroll
  useEffect(() => {
    if (!contextMenu) return;
    const close = () => setContextMenu(null);
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") close();
    };
    window.addEventListener("scroll", close, true);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("scroll", close, true);
      window.removeEventListener("keydown", onKey);
    };
  }, [contextMenu]);

  // Handle shutdown postgres via pgctld Stop
  const handleShutdownPostgres = useCallback(async () => {
    if (!revokeTarget?.id) return;
    setRevoking(true);
    setRevokeError(null);
    try {
      await api.stopPostgres(revokeTarget.id);
      setRevokeTarget(null);
    } catch (err) {
      setRevokeError(
        err instanceof Error ? err.message : "Failed to stop postgres",
      );
    } finally {
      setRevoking(false);
    }
  }, [api, revokeTarget]);

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
        <span className="ml-2 text-muted-foreground">Loading topology...</span>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex items-center justify-center h-full">
        <p className="text-destructive">{error}</p>
      </div>
    );
  }

  if (nodes.length === 0) {
    return (
      <div className="flex items-center justify-center h-full">
        <p className="text-muted-foreground">No topology data available</p>
      </div>
    );
  }

  return (
    <div className={cn("w-full h-full font-mono")}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        fitViewOptions={{ padding: 0.4 }}
        fitView
        className="bg-background"
        onNodeContextMenu={handleNodeContextMenu}
        onPaneClick={handlePaneClick}
      >
        <Background
          color="var(--foreground)"
          className="opacity-25"
          variant={BackgroundVariant.Dots}
          gap={16}
        />
      </ReactFlow>

      {/* Context menu for primary nodes */}
      {contextMenu && (
        <div
          className="fixed z-50 min-w-[180px] rounded-md border bg-popover p-1 shadow-md"
          style={{ left: contextMenu.x, top: contextMenu.y }}
        >
          <div className="px-2 py-1.5 text-xs font-medium text-muted-foreground">
            {contextMenu.pooler.id?.name}
          </div>
          <div className="h-px bg-border -mx-1 my-1" />
          <button
            className="flex w-full cursor-default items-center rounded-sm px-2 py-1.5 text-sm text-destructive hover:bg-destructive/10 outline-none"
            onClick={() => {
              setRevokeTarget(contextMenu.pooler);
              setContextMenu(null);
            }}
          >
            Shutdown Postgres
          </button>
        </div>
      )}

      {/* Revoke confirmation dialog */}
      <AlertDialog
        open={!!revokeTarget}
        onOpenChange={(open) => {
          if (!open) {
            setRevokeTarget(null);
            setRevokeError(null);
          }
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Shutdown Postgres</AlertDialogTitle>
            <AlertDialogDescription>
              This will stop PostgreSQL on{" "}
              <span className="font-semibold text-foreground">
                {revokeTarget?.id?.name}
              </span>{" "}
              in cell{" "}
              <span className="font-semibold text-foreground">
                {revokeTarget?.id?.cell}
              </span>
              . If this is the primary, the orchestrator will detect it and
              appoint a new primary automatically.
            </AlertDialogDescription>
          </AlertDialogHeader>
          {revokeError && (
            <p className="text-sm text-destructive">{revokeError}</p>
          )}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={revoking}>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleShutdownPostgres}
              disabled={revoking}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {revoking ? "Shutting down..." : "Shutdown"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

export default TopologyGraph;
