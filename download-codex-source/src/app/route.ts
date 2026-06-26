import { mirroredResponse } from "@/lib/mirror";

export const dynamic = "force-dynamic";

export async function GET(request: Request) {
  return mirroredResponse(request);
}

export async function HEAD(request: Request) {
  const response = await mirroredResponse(request);
  return new Response(null, {
    status: response.status,
    headers: response.headers,
  });
}
