const http = require("http");
const https = require("https");
const crypto = require("crypto");

const SALT = "59cf53e54c78";
const VERSION = "2.1.123";
const IDENTITY =
  "You are [[PERSON_2]] Code, Anthropic's official CLI for [[PERSON_2]].";
const BILLING_PREFIX = "x-anthropic-billing-header:";
const PORT = parseInt(process.env.SIDECAR_PORT || "8081", 10);
const UPSTREAM = "https://api.anthropic.com";

function extractFirstUserText(body) {
  const msgs = body.messages;
  if (!Array.isArray(msgs)) return "";
  const user = msgs.find((m) => m.role === "user" && !m.isMeta);
  if (!user) return "";
  const c = user.content;
  if (typeof c === "string") return c;
  if (Array.isArray(c)) {
    const t = c.find((b) => b.type === "text");
    return t && t.text ? t.text : "";
  }
  return "";
}

function computeBuildHash(text) {
  const chars = [4, 7, 20]
    .map((i) => (text[i] ? text[i] : "0"))
    .join("");
  return crypto
    .createHash("sha256")
    .update(SALT + chars + VERSION)
    .digest("hex")
    .slice(0, 3);
}

function buildBillingHeader(text) {
  const buildHash = computeBuildHash(text);
  return `cc_version=${VERSION}.${buildHash}; cc_entrypoint=cli; cch=00000;`;
}

function injectBillingAndIdentity(body) {
  const firstText = extractFirstUserText(body);
  const billingStr = BILLING_PREFIX + " " + buildBillingHeader(firstText);

  if (!body.system) {
    body.system = [
      { type: "text", text: billingStr },
      { type: "text", text: IDENTITY },
    ];
    return;
  }

  if (typeof body.system === "string") {
    body.system = [
      { type: "text", text: billingStr },
      { type: "text", text: IDENTITY },
      { type: "text", text: body.system },
    ];
    return;
  }

  if (Array.isArray(body.system)) {
    const hasBilling = body.system.some(
      (b) =>
        b.type === "text" &&
        typeof b.text === "string" &&
        b.text.startsWith(BILLING_PREFIX),
    );
    if (hasBilling) return;

    const hasIdentity = body.system.some(
      (b) =>
        b.type === "text" &&
        typeof b.text === "string" &&
        b.text === IDENTITY,
    );

    body.system.unshift(
      { type: "text", text: billingStr },
      ...(hasIdentity ? [] : [{ type: "text", text: IDENTITY }]),
    );
  }
}

const HOP_HEADERS = new Set([
  "connection",
  "keep-alive",
  "transfer-encoding",
  "te",
  "upgrade",
  "host",
]);

function forwardHeaders(req) {
  const headers = {};
  for (const [k, v] of Object.entries(req.headers)) {
    if (!HOP_HEADERS.has(k.toLowerCase())) {
      headers[k] = v;
    }
  }
  // Keep Authorization: Bearer as-is. CLI uses Bearer for OAuth tokens.
  // Do NOT convert to x-api-key — Anthropic validates OAuth via Authorization header.
  return headers;
}

const agent = new https.Agent({ keepAlive: true, maxSockets: 10 });

const server = http.createServer((req, res) => {
  if (req.method === "GET" && req.url === "/health") {
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ status: "ok" }));
    return;
  }

  if (req.method !== "POST") {
    res.writeHead(405);
    res.end("method not allowed");
    return;
  }

  const chunks = [];
  req.on("data", (c) => chunks.push(c));
  req.on("end", () => {
    const raw = Buffer.concat(chunks);
    let body;
    try {
      body = JSON.parse(raw.toString());
    } catch {
      res.writeHead(400, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "invalid json" }));
      return;
    }

    injectBillingAndIdentity(body);

    const outBody = Buffer.from(JSON.stringify(body));
    const headers = forwardHeaders(req);
    headers["content-length"] = outBody.length.toString();

    const url = new URL(req.url, UPSTREAM);
    const opts = {
      hostname: url.hostname,
      port: 443,
      path: url.pathname + url.search,
      method: "POST",
      headers,
    };

    const upstream = https.request({ ...opts, agent }, (upRes) => {
      res.writeHead(upRes.statusCode, upRes.headers);
      upRes.pipe(res);
    });

    upstream.on("error", (err) => {
      console.error("upstream error:", err.message);
      if (!res.headersSent) {
        res.writeHead(502, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ error: "upstream error", detail: err.message }));
      }
    });

    upstream.write(outBody);
    upstream.end();
  });
});

server.listen(PORT, "0.0.0.0", () => {
  console.log(`sidecar listening on 0.0.0.0:${PORT}`);
});
