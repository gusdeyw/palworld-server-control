import http from "node:http";
import net from "node:net";
import fs from "node:fs";
import path from "node:path";

const httpPort = Number(process.env.SIM_REST_PORT || 8212);
const rconPort = Number(process.env.SIM_RCON_PORT || 25575);
const adminPassword = process.env.SIM_ADMIN_PASSWORD || "1234";
const saveDirectory = process.env.SIM_SAVE_DIR || "/saved";
const startedAt = Date.now();

let worldDay = 128;
let saveCount = 0;
let players = [
  player("Wina", "wina", "steam_sim_wina", 48, 34, 114),
  player("Raka", "raka", "steam_sim_raka", 42, 51, 87),
  player("Miko", "miko", "steam_sim_miko", 39, 62, 71),
];
const bannedPlayers = new Set();

fs.mkdirSync(saveDirectory, { recursive: true });
writeSave("simulator startup");

function player(name, accountName, userId, level, ping, buildingCount) {
  return {
    name,
    accountName,
    playerId: `SIM_${name.toUpperCase()}`,
    userId,
    ip: "127.0.0.1",
    ping,
    location_x: 112.5 + level,
    location_y: 78.25 + level,
    level,
    building_count: buildingCount,
  };
}

function log(message) {
  console.log(`${new Date().toISOString()} [PalworldSim] ${message}`);
}

function authorized(request) {
  const expected = `Basic ${Buffer.from(`admin:${adminPassword}`).toString("base64")}`;
  return request.headers.authorization === expected;
}

function json(response, status, value) {
  const body = JSON.stringify(value);
  response.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8",
    "Content-Length": Buffer.byteLength(body),
    "Cache-Control": "no-store",
  });
  response.end(body);
}

async function readJSON(request) {
  const chunks = [];
  let length = 0;
  for await (const chunk of request) {
    length += chunk.length;
    if (length > 64 * 1024) throw new Error("request body is too large");
    chunks.push(chunk);
  }
  if (!chunks.length) return {};
  return JSON.parse(Buffer.concat(chunks).toString("utf8"));
}

function currentMetrics() {
  const seconds = Math.floor((Date.now() - startedAt) / 1000);
  const fps = 59.2 + Math.sin(seconds / 13) * 0.65;
  return {
    serverfps: Math.round(fps * 10) / 10,
    currentplayernum: players.length,
    serverframetime: Math.round((1000 / fps) * 100) / 100,
    maxplayernum: 4,
    uptime: seconds,
    days: worldDay,
  };
}

function settings() {
  return {
    Difficulty: "None",
    DayTimeSpeedRate: 1,
    NightTimeSpeedRate: 1,
    ExpRate: 1.5,
    PalCaptureRate: 1.2,
    PalSpawnNumRate: 1,
    DeathPenalty: "Item",
    bIsPvP: false,
    bEnableFriendlyFire: false,
    ServerPlayerMaxNum: 4,
    CrossplayPlatforms: ["Steam", "Xbox", "PS5", "Mac"],
    bIsUseBackupSaveData: true,
    RESTAPIEnabled: true,
    RESTAPIPort: httpPort,
    RCONEnabled: true,
    RCONPort: rconPort,
  };
}

function writeSave(reason) {
  saveCount += 1;
  const content = {
    simulator: true,
    savedAt: new Date().toISOString(),
    reason,
    saveCount,
    worldDay,
    players: players.map(({ name, userId, level }) => ({ name, userId, level })),
    bannedPlayers: [...bannedPlayers],
  };
  fs.writeFileSync(path.join(saveDirectory, "Level.sav"), JSON.stringify(content, null, 2));
  log(`World saved (${reason}, save ${saveCount})`);
}

function removePlayer(userId, ban) {
  const found = players.find((entry) => entry.userId === userId);
  players = players.filter((entry) => entry.userId !== userId);
  if (ban) bannedPlayers.add(userId);
  return found;
}

const api = http.createServer(async (request, response) => {
  const url = new URL(request.url || "/", `http://${request.headers.host || "localhost"}`);
  if (url.pathname === "/health") {
    json(response, 200, { ok: true });
    return;
  }
  if (!authorized(request)) {
    response.setHeader("WWW-Authenticate", 'Basic realm="PalworldSim"');
    json(response, 401, { error: "Unauthorized" });
    return;
  }

  try {
    if (request.method === "GET" && url.pathname === "/v1/api/info") {
      json(response, 200, {
        version: "1.0.0-simulator",
        servername: "Palpagos After Hours",
        description: "Local Palworld server simulation",
        worldguid: "LOCAL-SIMULATED-WORLD",
      });
      return;
    }
    if (request.method === "GET" && url.pathname === "/v1/api/metrics") {
      json(response, 200, currentMetrics());
      return;
    }
    if (request.method === "GET" && url.pathname === "/v1/api/players") {
      json(response, 200, { players });
      return;
    }
    if (request.method === "GET" && url.pathname === "/v1/api/settings") {
      json(response, 200, settings());
      return;
    }

    if (request.method === "POST" && url.pathname === "/v1/api/save") {
      writeSave("REST API request");
      json(response, 200, { success: true });
      return;
    }
    if (request.method === "POST" && url.pathname === "/v1/api/announce") {
      const body = await readJSON(request);
      log(`Announcement: ${String(body.message || "")}`);
      json(response, 200, { success: true });
      return;
    }
    if (request.method === "POST" && (url.pathname === "/v1/api/kick" || url.pathname === "/v1/api/ban")) {
      const body = await readJSON(request);
      const ban = url.pathname.endsWith("/ban");
      const removed = removePlayer(String(body.userid || ""), ban);
      log(`${ban ? "Banned" : "Kicked"} player ${removed?.name || body.userid || "unknown"}`);
      json(response, 200, { success: true });
      return;
    }
    if (request.method === "POST" && url.pathname === "/v1/api/unban") {
      const body = await readJSON(request);
      bannedPlayers.delete(String(body.userid || ""));
      log(`Unbanned player ${body.userid || "unknown"}`);
      json(response, 200, { success: true });
      return;
    }
    if (request.method === "POST" && url.pathname === "/v1/api/shutdown") {
      const body = await readJSON(request);
      const waitTime = Math.max(0, Math.min(60, Number(body.waittime || 0)));
      writeSave("graceful shutdown");
      log(`Shutdown scheduled in ${waitTime} seconds: ${String(body.message || "")}`);
      json(response, 200, { success: true });
      setTimeout(() => process.exit(0), waitTime * 1000);
      return;
    }
    if (request.method === "POST" && url.pathname === "/v1/api/stop") {
      log("Force stop requested");
      json(response, 200, { success: true });
      setTimeout(() => process.exit(0), 100);
      return;
    }

    json(response, 404, { error: "Unknown simulator endpoint" });
  } catch (error) {
    log(`Request error: ${error.message}`);
    json(response, 400, { error: error.message });
  }
});

api.listen(httpPort, "0.0.0.0", () => {
  log(`REST API listening on port ${httpPort}`);
});

const rcon = net.createServer((socket) => {
  let buffer = Buffer.alloc(0);
  let authenticated = false;

  socket.on("data", (chunk) => {
    buffer = Buffer.concat([buffer, chunk]);
    while (buffer.length >= 4) {
      const size = buffer.readInt32LE(0);
      if (size < 10 || size > 4 * 1024 * 1024) {
        socket.destroy();
        return;
      }
      const packetLength = size + 4;
      if (buffer.length < packetLength) return;
      const packet = buffer.subarray(0, packetLength);
      buffer = buffer.subarray(packetLength);

      const id = packet.readInt32LE(4);
      const type = packet.readInt32LE(8);
      const body = packet.subarray(12, packetLength - 2).toString("utf8");

      if (type === 3) {
        if (body === adminPassword) {
          authenticated = true;
          writeRCON(socket, id, 0, "");
          writeRCON(socket, id, 2, "");
          log("RCON client authenticated");
        } else {
          writeRCON(socket, -1, 2, "");
        }
        continue;
      }

      if (type === 2) {
        if (!authenticated) {
          writeRCON(socket, -1, 0, "Authentication required");
          continue;
        }
        const output = executeCommand(body);
        writeRCON(socket, id, 0, output);
      }
    }
  });
});

function writeRCON(socket, id, type, body) {
  const bodyBuffer = Buffer.from(body, "utf8");
  const size = 10 + bodyBuffer.length;
  const packet = Buffer.alloc(size + 4);
  packet.writeInt32LE(size, 0);
  packet.writeInt32LE(id, 4);
  packet.writeInt32LE(type, 8);
  bodyBuffer.copy(packet, 12);
  packet.writeInt16LE(0, packet.length - 2);
  socket.write(packet);
}

function executeCommand(command) {
  const trimmed = command.trim();
  log(`RCON command: ${trimmed}`);
  if (/^info$/i.test(trimmed)) {
    const metrics = currentMetrics();
    return `Palpagos After Hours | ${metrics.currentplayernum}/4 players | ${metrics.serverfps} FPS`;
  }
  if (/^showplayers$/i.test(trimmed)) {
    return players.map((entry) => `${entry.name},${entry.userId},${entry.ping}ms`).join("\n") || "No players online";
  }
  if (/^save$/i.test(trimmed)) {
    writeSave("RCON command");
    return "World saved";
  }
  if (/^broadcast\s+/i.test(trimmed)) {
    log(`Broadcast: ${trimmed.replace(/^broadcast\s+/i, "")}`);
    return "Broadcast sent";
  }
  return `Simulator received: ${trimmed}`;
}

rcon.listen(rconPort, "0.0.0.0", () => {
  log(`RCON listening on port ${rconPort}`);
});

setInterval(() => {
  worldDay += 1;
  const metrics = currentMetrics();
  log(`Tick healthy: ${metrics.serverfps} FPS, ${players.length}/4 players`);
}, 60_000).unref();

process.on("SIGTERM", () => {
  writeSave("container stop");
  log("Simulator stopped");
  process.exit(0);
});

