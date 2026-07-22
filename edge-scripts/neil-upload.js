/**
 * Neil Upload — Bunny Edge Script
 *
 * Proxies media uploads from the Neil Mattermost plugin to a BunnyCDN Storage Zone,
 * keeping the BunnyCDN API key server-side and out of the Mattermost plugin config.
 *
 * Route: PUT /upload/{path}
 *   - {path} is the storage path, e.g. neil/2025-01-15/12345.oga
 *
 * Required environment variables (set in the Bunny Edge Script dashboard):
 *   UPLOAD_SECRET        Shared secret sent by the plugin in the Authorization header
 *   STORAGE_ZONE         BunnyCDN storage zone name
 *   STORAGE_API_KEY      BunnyCDN storage zone API key (Password on the FTP & API Access tab)
 *   STORAGE_HOSTNAME     Storage zone hostname, e.g. storage.bunnycdn.com (EU) or de.storage.bunnycdn.com
 *   CDN_HOSTNAME         Pull zone hostname, e.g. assets.example.b-cdn.net (no trailing slash)
 */

import * as BunnySDK from "@bunny.net/edgescript-sdk";

BunnySDK.serve(async (request) => {
    if (request.method !== "PUT") {
        return new Response(JSON.stringify({ error: "Method not allowed" }), {
            status: 405,
            headers: { "Content-Type": "application/json" },
        });
    }

    const uploadSecret = process.env.UPLOAD_SECRET ?? "";
    const authHeader = request.headers.get("Authorization") ?? "";
    if (!uploadSecret || authHeader !== uploadSecret) {
        return new Response(JSON.stringify({ error: "Unauthorized" }), {
            status: 401,
            headers: { "Content-Type": "application/json" },
        });
    }

    const url = new URL(request.url);
    const match = url.pathname.match(/^\/upload\/(.+)$/);
    if (!match) {
        return new Response(JSON.stringify({ error: "Invalid path — expected /upload/{path}" }), {
            status: 400,
            headers: { "Content-Type": "application/json" },
        });
    }
    const filePath = match[1];

    const storageZone = process.env.STORAGE_ZONE ?? "";
    const storageKey = process.env.STORAGE_API_KEY ?? "";
    const storageHostname = process.env.STORAGE_HOSTNAME ?? "storage.bunnycdn.com";
    const cdnHostname = process.env.CDN_HOSTNAME ?? "";

    if (!storageZone || !storageKey || !cdnHostname) {
        return new Response(JSON.stringify({ error: "Edge Script not fully configured" }), {
            status: 503,
            headers: { "Content-Type": "application/json" },
        });
    }

    const storageURL = `https://${storageHostname}/${storageZone}/${filePath}`;
    const body = await request.arrayBuffer();

    const uploadResp = await fetch(storageURL, {
        method: "PUT",
        headers: {
            AccessKey: storageKey,
            "Content-Type": "application/octet-stream",
        },
        body,
    });

    if (!uploadResp.ok) {
        const errText = await uploadResp.text();
        return new Response(
            JSON.stringify({ error: `Storage upload failed (${uploadResp.status}): ${errText}` }),
            { status: 502, headers: { "Content-Type": "application/json" } }
        );
    }

    const cdnURL = `https://${cdnHostname}/${filePath}`;
    return new Response(JSON.stringify({ url: cdnURL }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
    });
});
