import { NextRequest, NextResponse } from "next/server";

const MULTIADMIN_API_URL =
  process.env.MULTIADMIN_API_URL || "http://localhost:15000";

// Hardcoded offset for prototyping: pgctld HTTP = multipooler HTTP + 1000
const PGCTLD_HTTP_PORT_OFFSET = 1000;

// POST /api/proxy-pooler/[cell]/[name]/kill-postgres
// Proxies Kill requests to the co-located pgctld's gRPC-gateway endpoint.
export async function POST(
  request: NextRequest,
  { params }: { params: Promise<{ cell: string; name: string }> },
) {
  const { cell, name } = await params;

  // Look up the pooler's address from the multiadmin backend
  const poolersRes = await fetch(
    `${MULTIADMIN_API_URL}/api/v1/poolers?cells=${encodeURIComponent(cell)}`,
  );
  if (!poolersRes.ok) {
    return NextResponse.json(
      { error: "Failed to fetch poolers from backend" },
      { status: 502 },
    );
  }

  const poolersData = await poolersRes.json();
  const pooler = poolersData.poolers?.find(
    (p: { id?: { name?: string } }) => p.id?.name === name,
  );

  if (!pooler?.hostname || !pooler?.port_map?.http) {
    return NextResponse.json(
      { error: `Pooler ${cell}/${name} not found or missing HTTP port` },
      { status: 404 },
    );
  }

  // Compute pgctld HTTP port from multipooler HTTP port
  const pgctldHttpPort = pooler.port_map.http + PGCTLD_HTTP_PORT_OFFSET;
  const pgctldUrl = `http://${pooler.hostname}:${pgctldHttpPort}/api/v1/postgres/kill`;
  const body = await request.text();

  const pgctldRes = await fetch(pgctldUrl, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: body || "{}",
  });

  const responseBody = await pgctldRes.text();
  return new NextResponse(responseBody, {
    status: pgctldRes.status,
    headers: { "Content-Type": "application/json" },
  });
}
