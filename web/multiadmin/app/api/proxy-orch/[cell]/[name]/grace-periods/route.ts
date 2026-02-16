import { NextRequest, NextResponse } from "next/server";

const MULTIADMIN_API_URL =
  process.env.MULTIADMIN_API_URL || "http://localhost:15000";

// GET /api/proxy-orch/[cell]/[name]/grace-periods
// Proxies grace period requests to the multiorch HTTP endpoint.
export async function GET(
  _request: NextRequest,
  { params }: { params: Promise<{ cell: string; name: string }> },
) {
  const { cell, name } = await params;

  // Look up the orch's address from the multiadmin backend
  const orchsRes = await fetch(
    `${MULTIADMIN_API_URL}/api/v1/orchs?cells=${encodeURIComponent(cell)}`,
  );
  if (!orchsRes.ok) {
    return NextResponse.json(
      { error: "Failed to fetch orchs from backend" },
      { status: 502 },
    );
  }

  const orchsData = await orchsRes.json();
  const orch = orchsData.orchs?.find(
    (o: { id?: { name?: string } }) => o.id?.name === name,
  );

  if (!orch?.hostname || !orch?.port_map?.http) {
    return NextResponse.json(
      { error: `Orch ${cell}/${name} not found or missing HTTP port` },
      { status: 404 },
    );
  }

  const orchUrl = `http://${orch.hostname}:${orch.port_map.http}/api/v1/grace-periods`;

  const orchRes = await fetch(orchUrl);

  const responseBody = await orchRes.text();
  return new NextResponse(responseBody, {
    status: orchRes.status,
    headers: { "Content-Type": "application/json" },
  });
}
