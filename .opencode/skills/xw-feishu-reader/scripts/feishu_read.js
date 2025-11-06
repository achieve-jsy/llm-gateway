#!/usr/bin/env node
// feishu_read.js — Read a Feishu docx document as complete Markdown.
//
// Supports:
//   - Plain text blocks (paragraph, headings, lists, code, dividers)
//   - Embedded spreadsheet blocks (block_type=30): fetched via Sheets API
//   - Quote containers (block_type=34)
//   - Wiki URLs: automatically resolved to docx token via Wiki Node API
//
// Usage:
//   node feishu_read.js --url <feishu_url>  [--out <output.md>]
//   node feishu_read.js --token <doc_token> [--out <output.md>]
//
// <feishu_url> accepts any of:
//   https://xxx.feishu.cn/wiki/Aa3PwfCXxx   (Wiki page)
//   https://xxx.feishu.cn/docx/Z7xqdIQrexx   (Docx page)
//
// Requirements:
//   - Windows or macOS
//     Windows: reads AES key from Windows Credential Manager via PowerShell
//     macOS:   reads AES key from macOS Keychain via `security` CLI
//   - lark-mcp previously authenticated via: npx @larksuiteoapi/lark-mcp mcp
//   - Node.js >= 16

"use strict";

const fs = require("fs");
const path = require("path");
const os = require("os");
const crypto = require("crypto");
const https = require("https");
const { execSync, spawnSync } = require("child_process");

const PLATFORM = os.platform(); // 'win32' | 'darwin' | 'linux'

// ─── CLI ──────────────────────────────────────────────────────────────────────

function parseArgs() {
  const args = process.argv.slice(2);
  const r = {
    url: null,
    token: null,
    out: null,
    // Aliyun OSS options (all optional; if omitted images fall back to feishu-image://TOKEN)
    ossEndpoint: null, // e.g. oss-cn-hangzhou.aliyuncs.com
    ossBucket: null, // bucket name
    ossKeyId: null, // AccessKeyId
    ossKeySecret: null, // AccessKeySecret
    ossPrefix: "doc-ai", // object key prefix
    ossPublicUrl: null, // custom CDN base URL (default: https://{bucket}.{endpoint})
  };
  for (let i = 0; i < args.length; i++) {
    if (args[i] === "--url" && args[i + 1]) r.url = args[++i];
    else if (args[i] === "--token" && args[i + 1]) r.token = args[++i];
    else if (args[i] === "--out" && args[i + 1]) r.out = args[++i];
    else if (args[i] === "--oss-endpoint" && args[i + 1])
      r.ossEndpoint = args[++i];
    else if (args[i] === "--oss-bucket" && args[i + 1]) r.ossBucket = args[++i];
    else if (args[i] === "--oss-key-id" && args[i + 1]) r.ossKeyId = args[++i];
    else if (args[i] === "--oss-key-secret" && args[i + 1])
      r.ossKeySecret = args[++i];
    else if (args[i] === "--oss-prefix" && args[i + 1]) r.ossPrefix = args[++i];
    else if (args[i] === "--oss-public-url" && args[i + 1])
      r.ossPublicUrl = args[++i];
  }
  if (!r.url && !r.token)
    fatal(
      "Usage: node feishu_read.js --url <url>  OR  --token <docToken>  [--out output.md]\n" +
        "  Optional OSS upload (converts Feishu images to public URLs):\n" +
        "    --oss-endpoint <endpoint>    e.g. oss-cn-hangzhou.aliyuncs.com\n" +
        "    --oss-bucket <bucket>        OSS bucket name\n" +
        "    --oss-key-id <id>            Aliyun AccessKeyId\n" +
        "    --oss-key-secret <secret>    Aliyun AccessKeySecret\n" +
        "    --oss-prefix <prefix>        Object key prefix (default: doc-ai)\n" +
        "    --oss-public-url <url>       Custom CDN base URL (default: https://{bucket}.{endpoint})"
    );
  if (!r.out) r.out = "feishu_read_output.md";
  return r;
}

// ─── Built-in OSS Defaults ────────────────────────────────────────────────────
// Images in Feishu documents are automatically uploaded to OSS and replaced
// with permanent public URLs. Override via CLI args if needed.
const DEFAULT_OSS = {
  endpoint: "oss-cn-hangzhou.aliyuncs.com",
  bucket: "harbor-yw",
  keyId: "LTAI5tCzf8WVY7DzfTLHRq1z",
  keySecret: "UAfjKRdX6FI4ngdN02rvV84JRWqteM",
  prefix: "doc-ai",
};

function ossConfigFromArgs(args) {
  // Use CLI args if all four are provided; otherwise fall back to built-in defaults.
  const endpoint = args.ossEndpoint || DEFAULT_OSS.endpoint;
  const bucket = args.ossBucket || DEFAULT_OSS.bucket;
  const keyId = args.ossKeyId || DEFAULT_OSS.keyId;
  const keySecret = args.ossKeySecret || DEFAULT_OSS.keySecret;
  const prefix = args.ossPrefix || DEFAULT_OSS.prefix;
  return {
    endpoint,
    bucket,
    keyId,
    keySecret,
    prefix,
    publicBase: args.ossPublicUrl || `https://${bucket}.${endpoint}`,
  };
}

function fatal(msg) {
  console.error("[error]", msg);
  process.exit(1);
}

// ─── App Credentials ─────────────────────────────────────────────────────────

function getAppCredentials() {
  const mcpJsonPath = path.join(process.cwd(), ".cursor", "mcp.json");
  if (fs.existsSync(mcpJsonPath)) {
    try {
      const mcpJson = JSON.parse(fs.readFileSync(mcpJsonPath, "utf8"));
      const larkArgs = mcpJson?.mcpServers?.["lark-feishu"]?.args || [];
      const aIdx = larkArgs.indexOf("-a");
      const sIdx = larkArgs.indexOf("-s");
      const appId = aIdx !== -1 ? larkArgs[aIdx + 1] : null;
      const appSecret = sIdx !== -1 ? larkArgs[sIdx + 1] : null;
      if (appId && appSecret) return { appId, appSecret };
    } catch (_) {
      /* ignore */
    }
  }
  return {
    appId: "cli_a90b5c88df769bd8",
    appSecret: "ab8mPg6b5XMi35eWone4dbysVeJkZPhJ",
  };
}

// ─── Auto Re-authenticate ─────────────────────────────────────────────────────
// Token 过期时强制调用 npx lark-mcp login 弹出浏览器授权，不使用飞书 MCP 自动获取。
// 固定使用下方 app_id / app_secret，不读取 mcp.json，确保每次都是同一应用授权。
const LOGIN_APP_ID = "cli_a90b5c88df769bd8";
const LOGIN_APP_SECRET = "ab8mPg6b5XMi35eWone4dbysVeJkZPhJ";

/** 飞书 API 返回的 token 过期错误码（如 99991663），检测到后直接触发 login 并重试，不 fallback 到 MCP */
const TOKEN_EXPIRED_CODE = 99991663;

const LOGIN_CMD_MANUAL =
  "npx -y @larksuiteoapi/lark-mcp login -a cli_a90b5c88df769bd8 -s ab8mPg6b5XMi35eWone4dbysVeJkZPhJ";

function reAuthenticate() {
  console.log(
    "\n[auth] Token expired — forcing lark-mcp login (browser OAuth)..."
  );
  console.log(`[auth] App ID: ${LOGIN_APP_ID}`);
  const result = spawnSync(
    "npx",
    [
      "-y",
      "@larksuiteoapi/lark-mcp",
      "login",
      "-a",
      LOGIN_APP_ID,
      "-s",
      LOGIN_APP_SECRET,
    ],
    { stdio: "inherit", timeout: 180000 }
  );
  if (result.status !== 0) {
    console.error("\n[auth] Login failed or timed out.");
    console.error("[auth] 请手动执行以下命令，在浏览器完成授权后重试读取：\n");
    console.error(`  ${LOGIN_CMD_MANUAL}\n`);
    fatal(
      "lark-mcp login failed or timed out. Please complete the OAuth flow in the browser (see command above)."
    );
  }
  console.log("[auth] Login completed, continuing...\n");
}

// ─── Auth ─────────────────────────────────────────────────────────────────────

/** Windows: read AES key from Windows Credential Manager via PowerShell P/Invoke */
function getAesKeyHexWindows() {
  const psScript = [
    'Add-Type @"',
    "using System; using System.Runtime.InteropServices; using System.Text;",
    "public class CredMgr {",
    '    [DllImport("advapi32.dll",SetLastError=true,CharSet=CharSet.Unicode)]',
    "    public static extern bool CredRead(string t,int y,int r,out IntPtr p);",
    '    [DllImport("advapi32.dll")] public static extern void CredFree(IntPtr p);',
    "    [StructLayout(LayoutKind.Sequential,CharSet=CharSet.Unicode)]",
    "    public struct CR { public int F,T; public string N,C;",
    "        public System.Runtime.InteropServices.ComTypes.FILETIME LW;",
    "        public int BS; public IntPtr B; public int P,AC; public IntPtr A; public string TA,U; }",
    "    public static byte[] Get(string t){ IntPtr p;",
    "        if(!CredRead(t,1,0,out p))return null;",
    "        try{ var c=(CR)Marshal.PtrToStructure(p,typeof(CR));",
    "             if(c.BS>0){var b=new byte[c.BS];Marshal.Copy(c.B,b,0,b.Length);return b;}}",
    "        finally{CredFree(p);} return null; }",
    "}",
    '"@',
    '$b=[CredMgr]::Get("lark-mcp/encryption-key")',
    'if($b){Write-Output(($b|ForEach-Object{$_.ToString("X2")})-join"")}else{Write-Output "NOTFOUND"}',
  ].join("\n");

  const result = spawnSync(
    "powershell",
    ["-NoProfile", "-NonInteractive", "-Command", psScript],
    { encoding: "utf8", timeout: 10000 }
  );
  const output = (result.stdout || "").trim().split("\n").pop().trim();
  if (!output || output === "NOTFOUND") {
    fatal(
      "AES key not found in Windows Credential Manager.\n" +
        'Fix: Re-run "npx @larksuiteoapi/lark-mcp mcp" and authenticate with Feishu.'
    );
  }
  return Buffer.from(
    output.match(/.{2}/g).map((h) => parseInt(h, 16))
  ).toString("ascii");
}

/**
 * macOS: reads AES key from macOS Keychain via the `security` CLI.
 * lark-mcp stores the key as a plain hex string in Keychain.
 */
function getAesKeyHexMac() {
  const result = spawnSync(
    "security",
    ["find-generic-password", "-s", "lark-mcp/encryption-key", "-w"],
    { encoding: "utf8", timeout: 10000 }
  );
  const output = (result.stdout || "").trim();
  if (result.status !== 0 || !output) {
    fatal(
      "AES key not found in macOS Keychain.\n" +
        'Fix: Re-run "npx @larksuiteoapi/lark-mcp mcp" and authenticate with Feishu.\n' +
        `security error: ${(result.stderr || "").slice(0, 300)}`
    );
  }
  return output;
}

/** Platform dispatcher for AES key retrieval */
function getAesKeyHex() {
  if (PLATFORM === "win32") return getAesKeyHexWindows();
  if (PLATFORM === "darwin") return getAesKeyHexMac();
  fatal(
    `Unsupported platform: "${PLATFORM}". Only Windows and macOS are supported.`
  );
}

/**
 * Returns the platform-appropriate path to lark-mcp's storage.json.
 *   Windows : %LOCALAPPDATA%\lark-mcp-nodejs\Data\storage.json
 *   macOS   : ~/Library/Application Support/lark-mcp-nodejs/Data/storage.json
 */
function getStoragePath() {
  if (PLATFORM === "win32") {
    return path.join(
      os.homedir(),
      "AppData",
      "Local",
      "lark-mcp-nodejs",
      "Data",
      "storage.json"
    );
  }
  if (PLATFORM === "darwin") {
    return path.join(
      os.homedir(),
      "Library",
      "Application Support",
      "lark-mcp-nodejs",
      "Data",
      "storage.json"
    );
  }
  fatal(
    `Unsupported platform: "${PLATFORM}". Only Windows and macOS are supported.`
  );
}

function getUserAccessToken(retrying = false) {
  const storagePath = getStoragePath();

  // Storage file missing → trigger login
  if (!fs.existsSync(storagePath)) {
    if (retrying)
      fatal(
        "lark-mcp storage still missing after login. Please check your Feishu authentication."
      );
    reAuthenticate();
    return getUserAccessToken(true);
  }

  const aesKeyHex = getAesKeyHex();
  const encrypted = fs.readFileSync(storagePath, "utf8").trim();
  const colonIdx = encrypted.indexOf(":");
  if (colonIdx === -1) fatal("Invalid storage.json format.");

  const iv = Buffer.from(encrypted.slice(0, colonIdx), "hex");
  const enc = encrypted.slice(colonIdx + 1);
  const key = Buffer.from(aesKeyHex, "hex");

  let decrypted;
  try {
    const decipher = crypto.createDecipheriv("aes-256-cbc", key, iv);
    decrypted = decipher.update(enc, "hex", "utf8") + decipher.final("utf8");
  } catch (e) {
    fatal(
      `Token decryption failed: ${e.message}\nFix: Re-authenticate via "npx @larksuiteoapi/lark-mcp mcp".`
    );
  }

  const data = JSON.parse(decrypted);
  const entries = Object.values(data.tokens || {});

  // No tokens or all expired → trigger login and retry once
  const now = Date.now() / 1000;
  const valid = entries.filter((t) => (t.expiresAt || 0) > now);
  if (!valid.length) {
    if (retrying)
      fatal(
        "Token still expired after login. Please complete the OAuth flow in the browser."
      );
    reAuthenticate();
    return getUserAccessToken(true);
  }

  return valid.sort((a, b) => (b.expiresAt || 0) - (a.expiresAt || 0))[0].token;
}

// ─── HTTPS Helper ─────────────────────────────────────────────────────────────

/**
 * GET with Bearer token. On Feishu API code TOKEN_EXPIRED_CODE (e.g. 99991663),
 * rejects with an error that has .code === TOKEN_EXPIRED_CODE so caller can
 * trigger reAuthenticate() and retry (do not fallback to Feishu MCP).
 */
function httpsGet(url, userToken) {
  return new Promise((resolve, reject) => {
    const opts = { headers: { Authorization: `Bearer ${userToken}` } };
    https
      .request(url, opts, (res) => {
        let data = "";
        res.on("data", (c) => (data += c));
        res.on("end", () => {
          try {
            const body = JSON.parse(data);
            if (body && body.code === TOKEN_EXPIRED_CODE) {
              const err = new Error(
                `Feishu API token expired (code=${TOKEN_EXPIRED_CODE}). Re-run login and retry.`
              );
              err.code = TOKEN_EXPIRED_CODE;
              reject(err);
              return;
            }
            resolve(body);
          } catch (e) {
            reject(
              new Error(
                `JSON parse error: ${e.message}\nBody: ${data.slice(0, 200)}`
              )
            );
          }
        });
      })
      .on("error", reject)
      .end();
  });
}

// ─── Image Download ───────────────────────────────────────────────────────────

/**
 * Download a Feishu image by its file token.
 * The API returns a 302 redirect to a time-limited CDN URL; we follow it.
 * Returns { data: Buffer, contentType: string }
 */
function downloadFeishuImage(imageToken, userToken) {
  return new Promise((resolve, reject) => {
    const apiUrl = `https://open.feishu.cn/open-apis/drive/v1/medias/${imageToken}/download`;

    function doGet(url, withAuth) {
      const opts = withAuth
        ? { headers: { Authorization: `Bearer ${userToken}` } }
        : {};
      const mod = url.startsWith("https") ? require("https") : require("http");
      mod
        .get(url, opts, (res) => {
          if (res.statusCode === 301 || res.statusCode === 302) {
            const location = res.headers.location;
            res.resume(); // discard body
            doGet(location, false); // CDN URL needs no auth
            return;
          }
          if (res.statusCode !== 200) {
            reject(
              new Error(`HTTP ${res.statusCode} downloading ${imageToken}`)
            );
            return;
          }
          const chunks = [];
          res.on("data", (c) => chunks.push(c));
          res.on("end", () =>
            resolve({
              data: Buffer.concat(chunks),
              contentType: (res.headers["content-type"] || "image/jpeg")
                .split(";")[0]
                .trim(),
            })
          );
        })
        .on("error", reject);
    }

    doGet(apiUrl, true);
  });
}

// ─── Aliyun OSS Upload ────────────────────────────────────────────────────────

/**
 * Check whether an object with the given key prefix already exists in OSS.
 * Uses ListObjects (GET bucket/?prefix=...) to find a matching key.
 * Returns the public URL if found, or null if not.
 *
 * The image object key is always "<prefix>/<token>.<ext>", so listing with
 * prefix="<prefix>/<token>" reliably finds it regardless of extension.
 */
function ossCheckExists(ossConfig, tokenPrefix) {
  return new Promise((resolve) => {
    const { endpoint, bucket, keyId, keySecret, publicBase } = ossConfig;
    const date = new Date().toUTCString();
    // CanonicalizedResource for bucket-level list is just "/{bucket}/"
    const canonicalizedResource = `/${bucket}/`;
    const stringToSign = ["GET", "", "", date, canonicalizedResource].join(
      "\n"
    );
    const sig = crypto
      .createHmac("sha1", keySecret)
      .update(stringToSign)
      .digest("base64");

    const qs = `?prefix=${encodeURIComponent(tokenPrefix)}&max-keys=1`;
    const opts = {
      method: "GET",
      hostname: `${bucket}.${endpoint}`,
      path: `/${qs}`,
      headers: {
        Host: `${bucket}.${endpoint}`,
        Date: date,
        Authorization: `OSS ${keyId}:${sig}`,
      },
    };

    require("https")
      .request(opts, (res) => {
        let body = "";
        res.on("data", (c) => (body += c));
        res.on("end", () => {
          // Parse XML: <Key>doc-ai/TOKEN.png</Key>
          const match = body.match(/<Key>([^<]+)<\/Key>/);
          if (match && match[1].startsWith(tokenPrefix)) {
            resolve(`${publicBase}/${match[1]}`);
          } else {
            resolve(null);
          }
        });
      })
      .on("error", () => resolve(null))
      .end();
  });
}

/**
 * Upload image bytes to Aliyun OSS using V1 (HMAC-SHA1) signature.
 * Returns the public URL of the uploaded object.
 */
function ossUpload(ossConfig, objectKey, data, contentType) {
  return new Promise((resolve, reject) => {
    const { endpoint, bucket, keyId, keySecret, publicBase } = ossConfig;
    const date = new Date().toUTCString();
    const md5 = crypto.createHash("md5").update(data).digest("base64");
    const canonicalizedResource = `/${bucket}/${objectKey}`;
    // x-oss-object-acl must be included in CanonicalizedOSSHeaders for V1 signature.
    const canonicalizedOSSHeaders = "x-oss-object-acl:public-read\n";
    const stringToSign = [
      "PUT",
      md5,
      contentType,
      date,
      canonicalizedOSSHeaders + canonicalizedResource,
    ].join("\n");
    const sig = crypto
      .createHmac("sha1", keySecret)
      .update(stringToSign)
      .digest("base64");

    const opts = {
      method: "PUT",
      hostname: `${bucket}.${endpoint}`,
      path: `/${objectKey}`,
      headers: {
        Host: `${bucket}.${endpoint}`,
        Date: date,
        "Content-Type": contentType,
        "Content-MD5": md5,
        "Content-Length": data.length,
        // Make each object publicly readable even when the bucket ACL is private.
        "x-oss-object-acl": "public-read",
        Authorization: `OSS ${keyId}:${sig}`,
      },
    };

    const req = require("https").request(opts, (res) => {
      let body = "";
      res.on("data", (c) => (body += c));
      res.on("end", () => {
        if (res.statusCode === 200) {
          resolve(`${publicBase}/${objectKey}`);
        } else {
          reject(
            new Error(
              `OSS upload failed: HTTP ${res.statusCode}\n${body.slice(0, 300)}`
            )
          );
        }
      });
    });
    req.on("error", reject);
    req.write(data);
    req.end();
  });
}

// ─── Image Processing ─────────────────────────────────────────────────────────

const CONTENT_TYPE_EXT = {
  "image/jpeg": "jpg",
  "image/jpg": "jpg",
  "image/png": "png",
  "image/gif": "gif",
  "image/webp": "webp",
  "image/svg+xml": "svg",
};

/**
 * For every unique image token in the block list:
 *   1. Check OSS: if an object with the same token prefix already exists, reuse it.
 *   2. Otherwise: download from Feishu (in-memory) → upload to OSS → release buffer.
 *   3. Return token → publicUrl map.
 *
 * Images are processed entirely in memory — no local temp files are written.
 * Buffers are released immediately after upload to minimise peak memory usage.
 * Falls back to feishu-image://TOKEN on any per-image error.
 */
async function processImages(blocks, userToken, ossConfig) {
  const imageTokens = [
    ...new Set(
      blocks
        .filter((b) => b.block_type === 27 && b.image && b.image.token)
        .map((b) => b.image.token)
    ),
  ];
  if (imageTokens.length === 0) return {};

  console.log(`Processing ${imageTokens.length} image(s) → OSS...`);
  const urlMap = {};
  let cached = 0;
  let uploaded = 0;

  for (const token of imageTokens) {
    process.stdout.write(`  [img] ${token.slice(0, 20)}... `);

    // ── Step 1: check OSS for an existing object with this token ──────────
    const tokenPrefix = `${ossConfig.prefix}/${token}`;
    const existingUrl = await ossCheckExists(ossConfig, tokenPrefix);
    if (existingUrl) {
      urlMap[token] = existingUrl;
      cached++;
      console.log(`CACHED (${existingUrl.split("/").pop()})`);
      continue;
    }

    // ── Step 2: download from Feishu (in-memory buffer) ───────────────────
    let imgData = null;
    try {
      const { data, contentType } = await downloadFeishuImage(token, userToken);
      imgData = { data, contentType };
    } catch (e) {
      console.warn(`DOWNLOAD FAILED → skip (${e.message})`);
      continue;
    }

    // ── Step 3: upload to OSS, then release the buffer immediately ─────────
    try {
      const ext = CONTENT_TYPE_EXT[imgData.contentType] || "jpg";
      const objectKey = `${tokenPrefix}.${ext}`;
      const sizeKB = (imgData.data.length / 1024).toFixed(1);
      const url = await ossUpload(
        ossConfig,
        objectKey,
        imgData.data,
        imgData.contentType
      );
      urlMap[token] = url;
      uploaded++;
      console.log(`OK (${sizeKB} KB)`);
    } catch (e) {
      console.warn(`UPLOAD FAILED → feishu-image:// fallback (${e.message})`);
    } finally {
      // Release the in-memory buffer as soon as it's no longer needed.
      imgData = null;
    }
  }

  const resolved = Object.keys(urlMap).length;
  console.log(
    `  ${resolved}/${imageTokens.length} resolved` +
      (cached ? `, ${cached} cached (OSS reused)` : "") +
      (uploaded ? `, ${uploaded} newly uploaded` : "")
  );
  return urlMap;
}

// ─── Token Resolution ─────────────────────────────────────────────────────────
// Input can be:
//   - Wiki URL:  https://xxx.feishu.cn/wiki/<wikiToken>
//   - Docx URL:  https://xxx.feishu.cn/docx/<docToken>
//   - Raw token: a plain alphanumeric/base62 string

// Returns { docToken, docType, title }
// docType: "docx" | "sheet"
async function resolveDocToken(urlOrToken, userToken) {
  // Raw token (no slashes/dots) — assume docx
  if (!urlOrToken.includes("/") && !urlOrToken.includes(".")) {
    return { docToken: urlOrToken, docType: "docx", title: "" };
  }

  // Docx URL
  const docxMatch = urlOrToken.match(/\/docx\/([A-Za-z0-9_-]+)/);
  if (docxMatch) return { docToken: docxMatch[1], docType: "docx", title: "" };

  // Wiki URL — can be docx or sheet
  const wikiMatch = urlOrToken.match(/\/wiki\/([A-Za-z0-9_-]+)/);
  if (wikiMatch) {
    const wikiToken = wikiMatch[1];
    console.log(`  Resolving Wiki token: ${wikiToken}`);
    const res = await httpsGet(
      `https://open.feishu.cn/open-apis/wiki/v2/spaces/get_node?token=${wikiToken}`,
      userToken
    );
    if (res.code !== 0)
      fatal(`Wiki API error: code=${res.code} msg=${res.msg}`);
    const node = res.data && res.data.node;
    if (!node) fatal("Wiki node not found in API response.");
    const docType = node.obj_type; // "docx" or "sheet"
    if (docType !== "docx" && docType !== "sheet")
      fatal(
        `Wiki node type "${docType}" is not supported. Supported types: "docx", "sheet".`
      );
    console.log(`  Wiki title: ${node.title}`);
    console.log(`  Type: ${docType}, Token: ${node.obj_token}`);
    return { docToken: node.obj_token, docType, title: node.title };
  }

  fatal(`Cannot resolve document token from: ${urlOrToken}`);
}

// ─── Feishu Blocks API ────────────────────────────────────────────────────────

async function fetchAllBlocks(userToken, docToken) {
  let allBlocks = [];
  let pageToken = "";
  do {
    const url =
      `https://open.feishu.cn/open-apis/docx/v1/documents/${docToken}/blocks?page_size=500` +
      (pageToken ? `&page_token=${encodeURIComponent(pageToken)}` : "");
    const res = await httpsGet(url, userToken);
    if (res.code !== 0)
      fatal(`Blocks API error: code=${res.code} msg=${res.msg}`);
    allBlocks = allBlocks.concat(res.data.items || []);
    pageToken = res.data.has_more ? res.data.page_token : "";
  } while (pageToken);
  return allBlocks;
}

// ─── Feishu Sheets API ────────────────────────────────────────────────────────

async function getSheetMeta(userToken, spreadsheetToken) {
  const url = `https://open.feishu.cn/open-apis/sheets/v3/spreadsheets/${spreadsheetToken}/sheets/query`;
  const res = await httpsGet(url, userToken); // rejects with .code=TOKEN_EXPIRED_CODE if expired
  if (res.code !== 0) {
    console.warn(
      `  [warn] Sheet meta error for ${spreadsheetToken}: code=${res.code}`
    );
    return [];
  }
  return res.data.sheets || [];
}

async function getSheetValues(userToken, spreadsheetToken, sheetId) {
  const range = encodeURIComponent(`${sheetId}!A1:Z500`);
  const url = `https://open.feishu.cn/open-apis/sheets/v2/spreadsheets/${spreadsheetToken}/values/${range}?valueRenderOption=ToString`;
  const res = await httpsGet(url, userToken); // rejects with .code=TOKEN_EXPIRED_CODE if expired
  if (res.code !== 0) {
    console.warn(
      `  [warn] Sheet values error for ${sheetId}: code=${res.code}`
    );
    return [];
  }
  return (res.data.valueRange || {}).values || [];
}

// Fetch ALL sheets for all unique spreadsheets referenced by type-30 blocks
async function fetchAllSheetData(userToken, blocks) {
  const sheetBlocks = blocks.filter(
    (b) => b.block_type === 30 && b.sheet && b.sheet.token
  );
  if (sheetBlocks.length === 0) return {};

  // Collect unique spreadsheet tokens and their referenced sheet IDs
  const spreadMap = {}; // spreadsheetToken → Set of sheetIds
  sheetBlocks.forEach((b) => {
    const full = b.sheet.token;
    const sep = full.lastIndexOf("_");
    const spreadToken = sep !== -1 ? full.substring(0, sep) : full;
    const sheetId = sep !== -1 ? full.substring(sep + 1) : "";
    if (!spreadMap[spreadToken]) spreadMap[spreadToken] = new Set();
    if (sheetId) spreadMap[spreadToken].add(sheetId);
  });

  const result = {}; // key: "spreadToken/sheetId" → rows[][]

  for (const [spreadToken, sheetIdSet] of Object.entries(spreadMap)) {
    console.log(
      `  Fetching spreadsheet: ${spreadToken} (${sheetIdSet.size} sheets)`
    );

    // Get sheet metadata to verify each sheetId exists
    const allSheets = await getSheetMeta(userToken, spreadToken);
    const validIds = new Set(allSheets.map((s) => s.sheet_id));

    for (const sheetId of sheetIdSet) {
      if (!validIds.has(sheetId)) {
        console.warn(
          `    [warn] Sheet ${sheetId} not found in spreadsheet meta, skipping.`
        );
        continue;
      }
      const rows = await getSheetValues(userToken, spreadToken, sheetId);
      result[`${spreadToken}/${sheetId}`] = rows;
    }
  }

  return result;
}

// ─── Standalone Spreadsheet Reader ───────────────────────────────────────────
// Reads all sheet tabs in a standalone Feishu spreadsheet document and renders
// them as Markdown tables (one H2 heading per sheet tab).

async function readSpreadsheetDocument(userToken, spreadsheetToken, title) {
  const sheets = await getSheetMeta(userToken, spreadsheetToken);
  if (sheets.length === 0) {
    console.warn("  [warn] No sheets found in spreadsheet.");
    return title ? `# ${title}\n\n(empty)\n` : "(empty)\n";
  }
  console.log(`  ${sheets.length} sheet tab(s) found`);

  let output = title ? `# ${title}\n\n` : "";

  for (const sheet of sheets) {
    console.log(`  Reading tab: ${sheet.title} (${sheet.sheet_id})`);
    output += `## ${sheet.title}\n\n`;
    const rows = await getSheetValues(
      userToken,
      spreadsheetToken,
      sheet.sheet_id
    );
    const md = matrixToMarkdown(rows);
    output += md ? md + "\n\n" : "(empty)\n\n";
  }

  return output;
}

// ─── Markdown Renderer ────────────────────────────────────────────────────────

// Block type → (markdown level or special keyword)
// Feishu block_type mapping (confirmed from API):
//   1  = Page (document root)
//   2  = Paragraph        field: text
//   3  = Heading1         field: heading1
//   4  = Heading2         field: heading2
//   5  = Heading3         field: heading3
//   6  = Heading4         field: heading4
//   7  = Heading5         field: heading5
//   8  = Heading6         field: heading6
//   9  = Heading7         field: heading7
//   10 = Heading8         field: heading8
//   11 = Heading9         field: heading9
//   12 = BulletedList     field: bullet
//   13 = OrderedList      field: ordered
//   14 = Code             field: code
//   22 = Divider
//   27 = Image            field: image.token / image.width / image.height
//   30 = Sheet (embedded spreadsheet)
//   34 = QuoteContainer

function extractText(elements) {
  if (!elements || !elements.length) return "";
  return elements
    .map((el) => {
      if (el.text_run) return el.text_run.content || "";
      if (el.mention_doc) return el.mention_doc.title || "";
      return "";
    })
    .join("");
}

/** Like extractText but wraps bold runs in **…** for rich rendering (used by quote/callout blocks). */
function extractRichText(elements) {
  if (!elements || !elements.length) return "";
  return elements
    .map((el) => {
      if (el.text_run) {
        const content = el.text_run.content || "";
        const bold =
          el.text_run.text_element_style && el.text_run.text_element_style.bold;
        if (bold && content.trim()) return "**" + content + "**";
        return content;
      }
      if (el.mention_doc) return el.mention_doc.title || "";
      return "";
    })
    .join("");
}

function getBlockText(b) {
  const t = b.block_type;
  if (t === 2) return extractText(b.text?.elements);
  if (t >= 3 && t <= 11) {
    const key = "heading" + (t - 2);
    return extractText(b[key]?.elements);
  }
  if (t === 12) return extractText(b.bullet?.elements);
  if (t === 13) return extractText(b.ordered?.elements);
  if (t === 14) return extractText(b.code?.elements);
  return "";
}

function matrixToMarkdown(rows) {
  if (!rows || !rows.length) return "";

  // Trim trailing all-empty rows
  let lastRow = rows.length - 1;
  while (
    lastRow > 0 &&
    rows[lastRow].every((c) => c === null || c === "" || c === undefined)
  )
    lastRow--;
  const trimmed = rows.slice(0, lastRow + 1);
  if (!trimmed.length) return "";

  // Find actual column count (trim right-side empty columns)
  let maxCols = 0;
  trimmed.forEach((r) => {
    let last = 0;
    for (let i = 0; i < r.length; i++) {
      if (r[i] !== null && r[i] !== "" && r[i] !== undefined) last = i + 1;
    }
    if (last > maxCols) maxCols = last;
  });
  if (maxCols === 0) return "";

  const padded = trimmed.map((r) => {
    const arr = r.slice(0, maxCols);
    while (arr.length < maxCols) arr.push("");
    return arr.map((c) =>
      String(c == null ? "" : c)
        .replace(/\|/g, "\\|")
        .replace(/\r?\n/g, " ")
        .trim()
    );
  });

  const lines = [
    "| " + padded[0].join(" | ") + " |",
    "| " + padded[0].map(() => "---").join(" | ") + " |",
    ...padded.slice(1).map((r) => "| " + r.join(" | ") + " |"),
  ];
  return lines.join("\n");
}

function renderBlocks(blocks, sheetsData, imageUrlMap = {}) {
  const blockMap = {};
  blocks.forEach((b) => (blockMap[b.block_id] = b));
  const visited = new Set();
  const outLines = [];

  function emit(line) {
    outLines.push(line);
  }

  function renderBlock(blockId, depth) {
    if (visited.has(blockId)) return;
    visited.add(blockId);
    const b = blockMap[blockId];
    if (!b) return;
    const t = b.block_type;

    // ── Document root: recurse only ──────────────────────────────────────────
    if (t === 1) {
      (b.children || []).forEach((id) => renderBlock(id, 0));
      return;
    }

    // ── Headings ─────────────────────────────────────────────────────────────
    if (t >= 3 && t <= 11) {
      const level = t - 2; // type3→H1, type4→H2, ...
      const text = getBlockText(b);
      if (text.trim()) {
        emit("");
        emit("#".repeat(level) + " " + text);
        emit("");
      }
      (b.children || []).forEach((id) => renderBlock(id, depth + 1));
      return;
    }

    // ── Paragraph ─────────────────────────────────────────────────────────────
    if (t === 2) {
      const text = getBlockText(b);
      emit(text.trim() ? text : "");
      (b.children || []).forEach((id) => renderBlock(id, depth + 1));
      return;
    }

    // ── Bulleted list ──────────────────────────────────────────────────────────
    if (t === 12) {
      const text = getBlockText(b);
      emit("  ".repeat(Math.max(0, depth)) + "- " + text);
      (b.children || []).forEach((id) => renderBlock(id, depth + 1));
      return;
    }

    // ── Ordered list ───────────────────────────────────────────────────────────
    if (t === 13) {
      const text = getBlockText(b);
      emit("  ".repeat(Math.max(0, depth)) + "1. " + text);
      (b.children || []).forEach((id) => renderBlock(id, depth + 1));
      return;
    }

    // ── Code block ─────────────────────────────────────────────────────────────
    if (t === 14) {
      const text = getBlockText(b);
      const lang = b.code?.style?.language || "";
      emit("```" + (lang > 1 ? String(lang) : ""));
      emit(text);
      emit("```");
      emit("");
      return;
    }

    // ── TodoList (block_type=16) ───────────────────────────────────────────────
    if (t === 16) {
      const done = b.todo && b.todo.style && b.todo.style.done;
      const text = extractText(b.todo && b.todo.elements);
      emit(
        "  ".repeat(Math.max(0, depth)) + (done ? "- [x] " : "- [ ] ") + text
      );
      (b.children || []).forEach((id) => renderBlock(id, depth + 1));
      return;
    }

    // ── Callout block (block_type=18) ──────────────────────────────────────────
    // High-light annotation block (带颜色/emoji). Content lives in children paragraphs.
    if (t === 18) {
      const emoji =
        b.callout && b.callout.emoji_id ? b.callout.emoji_id + " " : "";
      (b.children || []).forEach((cid) => {
        visited.add(cid);
        const cb = blockMap[cid];
        if (!cb) return;
        const text = extractRichText(
          (cb.text || cb.bullet || cb.ordered || {}).elements
        );
        if (text.trim()) emit("> " + emoji + text.replace(/\n/g, "\n> "));
        (cb.children || []).forEach((id) => renderBlock(id, depth + 1));
      });
      emit("");
      return;
    }

    // ── Divider (block_type=21 or 22) ─────────────────────────────────────────
    if (t === 21 || t === 22) {
      emit("");
      emit("---");
      emit("");
      return;
    }

    // ── Image (block_type=27) ──────────────────────────────────────────────────
    // If OSS upload succeeded → use public URL (standard Markdown, works with Import API).
    // Otherwise → preserve feishu-image://TOKEN as fallback.
    if (t === 27) {
      const token = b.image && b.image.token ? b.image.token : "";
      if (token) {
        emit("");
        if (imageUrlMap[token]) {
          emit(`![image](${imageUrlMap[token]})`);
        } else {
          const w = b.image.width || 0;
          const h = b.image.height || 0;
          emit(`![image](feishu-image://${token} "${w}x${h}")`);
        }
        emit("");
      }
      return;
    }
    // ── Grid / GridColumn (block_type=24, 25) ──────────────────────────────────
    // Layout containers — just recurse children; no special output.
    if (t === 24 || t === 25) {
      (b.children || []).forEach((id) => renderBlock(id, depth));
      return;
    }

    // ── Embedded Spreadsheet (block_type=30) ───────────────────────────────────
    if (t === 30 && b.sheet) {
      const full = b.sheet.token;
      const sep = full.lastIndexOf("_");
      const spreadToken = sep !== -1 ? full.substring(0, sep) : full;
      const sheetId = sep !== -1 ? full.substring(sep + 1) : "";
      const key = `${spreadToken}/${sheetId}`;
      const rows = sheetsData[key] || [];
      const md = matrixToMarkdown(rows);
      if (md) {
        emit("");
        emit(md);
        emit("");
      }
      // Do NOT recurse children for sheet blocks
      return;
    }

    // ── Quote block (block_type=15) ────────────────────────────────────────────
    // Text lives directly in b.quote.elements (no children). Supports bold runs.
    // Renders as blockquote lines, e.g.: > **职责说明：** ...
    if (t === 15) {
      const text = extractRichText(b.quote && b.quote.elements);
      if (text.trim()) {
        text.split("\n").forEach((line) => {
          if (line.trim()) emit("> " + line);
        });
        emit("");
      }
      return;
    }

    // ── Table (block_type=31) ──────────────────────────────────────────────────
    if (t === 31 && b.table) {
      const { row_size = 1, column_size = 1 } = b.table.property || {};
      const cellIds = b.table.cells || b.children || [];

      // Recursively extract text and image URLs from a block and all its children.
      function extractCellContent(blockId) {
        if (visited.has(blockId)) return [];
        visited.add(blockId);
        const blk = blockMap[blockId];
        if (!blk) return [];
        const parts = [];
        // Image block
        if (blk.block_type === 27 && blk.image && blk.image.token) {
          const imgUrl = imageUrlMap[blk.image.token];
          if (imgUrl) parts.push(`![](${imgUrl})`);
          return parts;
        }
        // Text block
        const text = getBlockText(blk);
        if (text.trim()) parts.push(text.trim());
        // Recurse children
        (blk.children || []).forEach((cid) => {
          parts.push(...extractCellContent(cid));
        });
        return parts;
      }

      const rows = [];
      for (let r = 0; r < row_size; r++) {
        const row = [];
        for (let c = 0; c < column_size; c++) {
          const cellId = cellIds[r * column_size + c];
          const cell = cellId ? blockMap[cellId] : null;
          if (cell) {
            visited.add(cellId);
            const textParts = [];
            (cell.children || []).forEach((childId) => {
              textParts.push(...extractCellContent(childId));
            });
            row.push(textParts.join(" "));
          } else {
            row.push("");
          }
        }
        rows.push(row);
      }

      if (rows.length > 0) {
        const md = matrixToMarkdown(rows);
        if (md) {
          emit("");
          emit(md);
          emit("");
        }
      }
      return;
    }

    // ── TableCell (block_type=32) ──────────────────────────────────────────────
    // Normally consumed by the type-31 handler above; this fallback handles
    // any orphaned cells that were not yet visited.
    if (t === 32) {
      (b.children || []).forEach((id) => renderBlock(id, depth));
      return;
    }

    // ── Quote container (block_type=34) ────────────────────────────────────────
    if (t === 34) {
      (b.children || []).forEach((cid) => {
        visited.add(cid);
        const cb = blockMap[cid];
        if (!cb) return;
        const text = getBlockText(cb);
        if (text.trim()) emit("> " + text.replace(/\n/g, "\n> "));
        (cb.children || []).forEach((id) => renderBlock(id, depth + 1));
      });
      return;
    }

    // ── Fallback: recurse children for unknown container types ─────────────────
    (b.children || []).forEach((id) => renderBlock(id, depth + 1));
  }

  // Find and render from the page root (block_type=1)
  const root = blocks.find((b) => b.block_type === 1);
  if (root) {
    renderBlock(root.block_id, 0);
  } else {
    // Fallback: render all top-level blocks
    blocks.forEach((b) => renderBlock(b.block_id, 0));
  }

  return outLines
    .join("\n")
    .replace(/\n{4,}/g, "\n\n\n")
    .trim();
}

// ─── Main ─────────────────────────────────────────────────────────────────────
// When Feishu API returns token expired (e.g. 99991663), we reAuthenticate() and
// retry once. Do not fallback to Feishu MCP — always use this script's login flow.

(async () => {
  const args = parseArgs();
  const ossConfig = ossConfigFromArgs(args);

  console.log("────────────────────────────────────────────────────────────");
  console.log("Feishu Document Reader");
  console.log("────────────────────────────────────────────────────────────");
  console.log(
    `OSS:   ${ossConfig.bucket}.${ossConfig.endpoint} / ${ossConfig.prefix}`
  );

  /** Full read flow with given token. Throws with .code === TOKEN_EXPIRED_CODE if API returns token expired. */
  async function doRead(userToken) {
    process.stdout.write("Resolving document token... ");
    const { docToken, docType, title } = await resolveDocToken(
      args.url || args.token,
      userToken
    );
    console.log(`OK → ${docToken} (type: ${docType})`);

    if (docType === "sheet") {
      console.log(`Reading spreadsheet document: ${title || docToken}`);
      const markdown = await readSpreadsheetDocument(
        userToken,
        docToken,
        title
      );
      const outPath = path.resolve(args.out);
      fs.mkdirSync(path.dirname(outPath), { recursive: true });
      fs.writeFileSync(outPath, markdown, "utf8");
      console.log(
        "────────────────────────────────────────────────────────────"
      );
      console.log("SUCCESS");
      console.log(`FILE:  ${outPath}`);
      console.log(
        `SIZE:  ${(Buffer.byteLength(markdown, "utf8") / 1024).toFixed(1)} KB`
      );
      console.log(
        "────────────────────────────────────────────────────────────"
      );
      return;
    }

    process.stdout.write("Fetching document blocks... ");
    const blocks = await fetchAllBlocks(userToken, docToken);
    console.log(`${blocks.length} blocks`);

    const typeCounts = {};
    blocks.forEach((b) => {
      typeCounts[b.block_type] = (typeCounts[b.block_type] || 0) + 1;
    });
    const imageCount = typeCounts[27] || 0;
    const sheetCount = typeCounts[30] || 0;
    console.log(
      `  Block types: ${Object.entries(typeCounts)
        .map(([k, v]) => `type${k}×${v}`)
        .join(", ")}`
    );

    let sheetsData = {};
    if (sheetCount > 0) {
      console.log(`Fetching ${sheetCount} embedded spreadsheet(s)...`);
      sheetsData = await fetchAllSheetData(userToken, blocks);
      const totalSheets = Object.keys(sheetsData).length;
      console.log(`  ${totalSheets} sheet(s) loaded`);
    } else {
      console.log("No embedded spreadsheets found.");
    }

    let imageUrlMap = {};
    if (imageCount > 0) {
      imageUrlMap = await processImages(blocks, userToken, ossConfig);
    }

    process.stdout.write("Rendering Markdown... ");
    const markdown = renderBlocks(blocks, sheetsData, imageUrlMap);
    console.log(`${markdown.split("\n").length} lines`);

    const outPath = path.resolve(args.out);
    fs.mkdirSync(path.dirname(outPath), { recursive: true });
    fs.writeFileSync(outPath, markdown, "utf8");

    const ossUploaded = Object.keys(imageUrlMap).length;
    console.log("────────────────────────────────────────────────────────────");
    console.log("SUCCESS");
    console.log(`FILE:  ${outPath}`);
    console.log(
      `SIZE:  ${(Buffer.byteLength(markdown, "utf8") / 1024).toFixed(1)} KB`
    );
    if (imageCount > 0) {
      const ossSkipped = imageCount - ossUploaded;
      console.log(
        `IMG:   ${imageCount} total` +
          (ossUploaded > 0 ? `, ${ossUploaded} uploaded to OSS` : "") +
          (ossSkipped > 0 ? `, ${ossSkipped} reused from OSS cache` : "")
      );
    }
    console.log("────────────────────────────────────────────────────────────");
  }

  process.stdout.write("Getting user token... ");
  let userToken = getUserAccessToken();
  console.log(`OK (${userToken.substring(0, 12)}...)`);

  try {
    await doRead(userToken);
  } catch (e) {
    if (e && e.code === TOKEN_EXPIRED_CODE) {
      reAuthenticate();
      process.stdout.write("Getting user token (after re-login)... ");
      userToken = getUserAccessToken(true);
      console.log(`OK (${userToken.substring(0, 12)}...)`);
      await doRead(userToken);
    } else {
      throw e;
    }
  }
})().catch((e) => {
  console.error("[fatal]", e.message || e);
  process.exit(1);
});
