// rvr pi hook extension.
//
// Loaded with `pi -e <this file>`. pi loads extensions through jiti as ES
// modules with a default-exported factory that receives the ExtensionAPI. This
// hook connects to the unix socket named by RVR_HOOK_SOCKET and reports pi
// lifecycle events to the rvr supervisor as newline-delimited JSON:
//   {"event": "agent_start", "ref": "<session file path>"}
//
// The session file path (used by rvr for `pi --session <ref>` resume) is not
// in the event payloads; it is read from ctx.sessionManager.getSessionFile().
import * as net from "node:net";

export default function (pi) {
  const socketPath = process.env.RVR_HOOK_SOCKET;
  let sock = null;
  if (socketPath) {
    sock = net.createConnection(socketPath);
    sock.on("error", () => {}); // never let a broken pipe crash the agent
  }

  const emit = (event, extra) => {
    if (!sock) return;
    try {
      sock.write(JSON.stringify({ event, ...(extra || {}) }) + "\n");
    } catch (_) {
      // best-effort; state reporting must never disrupt the agent
    }
  };

  const refOf = (ctx) => {
    try {
      return (ctx && ctx.sessionManager && ctx.sessionManager.getSessionFile()) || "";
    } catch (_) {
      return "";
    }
  };

  pi.on("session_start", (_event, ctx) => emit("session_start", { ref: refOf(ctx) }));
  pi.on("agent_start", (_event, ctx) => emit("agent_start", { ref: refOf(ctx) }));
  pi.on("agent_end", () => emit("agent_end"));
  pi.on("session_shutdown", () => emit("session_shutdown"));
}
