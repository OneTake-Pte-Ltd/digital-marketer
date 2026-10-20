/**
 * Neil Upload — Bunny Edge Script
 *
 * Stores Neil's media files in a BunnyCDN Storage Zone, keeping the BunnyCDN API key
 * server-side and out of the Mattermost plugin config.
 *
 * Route: POST /fetch/{path}
 *   - {path} is the storage path, e.g. neil/2025-01-15/12345.oga
 *   - Body: {"source_url": "https://api.telegram.org/file/bot.../voice/file_0.oga"}
 *   - The script downloads the file from Telegram itself and writes it to Storage, so the
 *     file never passes through the Mattermost server.
 *
 * Route: PUT /upload/{path}
 *   - Body: the raw file bytes. Kept for plugin versions older than the /fetch route.
 *
 * Both routes return {"url": "<public CDN URL>"}.
 *
 * Required environment variables (set in the Bunny Edge Script dashboard):
 *   UPLOAD_SECRET        Shared secret sent by the plugin in the Authorization header
 *   STORAGE_ZONE         BunnyCDN storage zone name
 *   STORAGE_API_KEY      BunnyCDN storage zone API key (Password on the FTP & API Access tab)
 *   STORAGE_HOSTNAME     Storage zone hostname, e.g. storage.bunnycdn.com (EU) or de.storage.bunnycdn.com
 *   CDN_HOSTNAME         Pull zone hostname, e.g. assets.example.b-cdn.net (no trailing slash)
 */

import * as BunnySDK from "@bunny.net/edgescript-sdk";
import process from "node:process";

// Only Telegram file downloads may be fetched, so the script cannot be used as an open proxy.
const ALLOWED_SOURCE_HOST = "api.telegram.org";

function jsonResponse(status, payload) {
    return new Response(JSON.stringify(payload), {
        status,
        headers: { "Content-Type": "application/json" },
    });
}

// Any uncaught error is returned as JSON so the plugin can report the real cause
// instead of an opaque Bunny 502 page.
BunnySDK.serve(async (request) => {
    try {
        return await handleRequest(request);
    } catch (err) {
        return jsonResponse(500, { error: `Edge Script error: ${err && err.message ? err.message : String(err)}` });
    }
});

async function handleRequest(request) {
    const url = new URL(request.url);
    const fetchMatch = url.pathname.match(/^\/fetch\/(.+)$/);
    const uploadMatch = url.pathname.match(/^\/upload\/(.+)$/);

    const isFetch = request.method === "POST" && fetchMatch;
    const isUpload = request.method === "PUT" && uploadMatch;
    if (!isFetch && !isUpload) {
        if (request.method !== "POST" && request.method !== "PUT") {
            return jsonResponse(405, { error: "Method not allowed" });
        }
        return jsonResponse(400, { error: "Invalid path; expected POST /fetch/{path} or PUT /upload/{path}" });
    }

    const uploadSecret = process.env.UPLOAD_SECRET ?? "";
    const authHeader = request.headers.get("Authorization") ?? "";
    if (!uploadSecret || authHeader !== uploadSecret) {
        return jsonResponse(401, { error: "Unauthorized" });
    }

    const storage = storageConfig();
    if (!storage) {
        return jsonResponse(503, { error: "Edge Script not fully configured" });
    }

    if (isUpload) {
        return storeFile(storage, uploadMatch[1], await request.arrayBuffer());
    }

    let sourceURL;
    try {
        const payload = await request.json();
        sourceURL = new URL(payload.source_url);
    } catch {
        return jsonResponse(400, { error: "Body must be JSON with a valid source_url" });
    }
    if (sourceURL.protocol !== "https:" || sourceURL.hostname !== ALLOWED_SOURCE_HOST) {
        return jsonResponse(400, { error: `source_url must be an https://${ALLOWED_SOURCE_HOST} URL` });
    }

    // The source URL embeds the Telegram bot token: never echo it back in an error.
    let sourceResp;
    try {
        sourceResp = await fetch(sourceURL.toString());
    } catch (err) {
        return jsonResponse(502, { error: "Telegram download failed: network error" });
    }
    if (!sourceResp.ok) {
        const status = sourceResp.status;
        // 5xx from Telegram is worth a retry by the plugin; anything else is not.
        return jsonResponse(status >= 500 ? 502 : 422, { error: `Telegram download failed with status ${status}` });
    }

    return storeFile(storage, fetchMatch[1], await sourceResp.arrayBuffer());
}

function storageConfig() {
    const zone = process.env.STORAGE_ZONE ?? "";
    const key = process.env.STORAGE_API_KEY ?? "";
    const hostname = process.env.STORAGE_HOSTNAME ?? "storage.bunnycdn.com";
    const cdnHostname = process.env.CDN_HOSTNAME ?? "";
    if (!zone || !key || !cdnHostname) {
        return null;
    }
    return { zone, key, hostname, cdnHostname };
}

async function storeFile(storage, filePath, body) {
    const storageURL = `https://${storage.hostname}/${storage.zone}/${filePath}`;
    const uploadResp = await fetch(storageURL, {
        method: "PUT",
        headers: {
            AccessKey: storage.key,
            "Content-Type": "application/octet-stream",
        },
        body,
    });

    if (!uploadResp.ok) {
        const errText = await uploadResp.text();
        return jsonResponse(502, { error: `Storage upload failed (${uploadResp.status}): ${errText}` });
    }

    return jsonResponse(200, { url: `https://${storage.cdnHostname}/${filePath}` });
}
