import { NextRequest, NextResponse } from "next/server";

const MULTIADMIN_API_URL =
  process.env.MULTIADMIN_API_URL || "http://localhost:15000";

// POST /api/proxy-pooler/[cell]/[name]/begin-term
// Proxies BeginTerm requests to the target multipooler's gRPC-gateway endpoint.
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

  // Forward the BeginTerm request to the pooler's gRPC-gateway endpoint
  const poolerUrl = `http://${pooler.hostname}:${pooler.port_map.http}/api/v1/consensus/begin-term`;
  const body = await request.text();

  const poolerRes = await fetch(poolerUrl, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body,
  });

  const responseBody = await poolerRes.text();
  return new NextResponse(responseBody, {
    status: poolerRes.status,
    headers: { "Content-Type": "application/json" },
  });
}
