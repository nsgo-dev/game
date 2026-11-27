import {
  EntityRecordFieldFlags,
  EntityRecordFieldKind,
  EntityRecordFieldProps,
  EntityRecordFieldX,
  EntityRecordFieldY,
  HandshakeMessageTagHello,
  HandshakeMessageTagReject,
  HandshakeMessageTagWelcome,
  Reader,
  ReplicationMessageTagDelta,
  ReplicationMessageTagFull,
  ReplicationMessageVersion,
  Writer,
  createEntityRecord,
  createHandshakeMessage,
  createReplicationMessage,
  decodeHandshakeMessage,
  encodeHandshakeMessage,
  encodeReplicationMessage,
  float32Bits,
} from "../../pkg/online/netplay/wire/ts/index.ts";
import { ReplicationStreamClient } from "./replication_client.ts";

function hexToBytes(hex: string): Uint8Array {
  const out = new Uint8Array(hex.length / 2);
  for (let i = 0; i < out.length; i++) {
    out[i] = parseInt(hex.slice(i * 2, i * 2 + 2), 16);
  }
  return out;
}

function bytesToHex(b: Uint8Array): string {
  let s = "";
  for (let i = 0; i < b.byteLength; i++) {
    s += b[i]!.toString(16).padStart(2, "0");
  }
  return s;
}

function serializeEntities(client: ReplicationStreamClient) {
  const ids = Array.from(client.entities.keys()).sort((a, b) => a - b);
  return ids.map((id) => {
    const e = client.entities.get(id)!;
    return {
      id: e.id,
      kind: e.kind,
      xBits: float32Bits(e.x),
      yBits: float32Bits(e.y),
      vxBits: float32Bits(e.vx),
      vyBits: float32Bits(e.vy),
      rotBits: float32Bits(e.rotation),
      flags: e.flags,
      propsHex: bytesToHex(e.props),
    };
  });
}

async function runE2EBridge(): Promise<void> {
  const client = new ReplicationStreamClient();
  const decoder = new TextDecoder();
  let buf = "";

  const processLine = (line: string): void => {
    const trimmed = line.trim();
    if (!trimmed) return;
    const cmd = JSON.parse(trimmed) as { hex: string };
    const res = client.handlePacket(hexToBytes(cmd.hex));
    const out = {
      kind: res.kind,
      epoch: client.epoch,
      tick: client.tick,
      replyHex: res.reply ? bytesToHex(res.reply) : "",
      entities: serializeEntities(client),
    };
    process.stdout.write(JSON.stringify(out) + "\n");
  };

  for await (const chunk of Bun.stdin.stream()) {
    buf += decoder.decode(chunk, { stream: true });
    let nl = buf.indexOf("\n");
    while (nl !== -1) {
      processLine(buf.slice(0, nl));
      buf = buf.slice(nl + 1);
      nl = buf.indexOf("\n");
    }
  }
  buf += decoder.decode();
  if (buf.trim().length > 0) {
    processLine(buf);
  }
}

async function runWebSocketClient(url: string, token: string): Promise<void> {
  const client = new ReplicationStreamClient();
  const ws = new WebSocket(url);
  ws.binaryType = "arraybuffer";
  let welcomed = false;
  let seq = 1;
  const txBuf = new Uint8Array(256);

  ws.onopen = () => {
    const hello = createHandshakeMessage();
    hello.tag = HandshakeMessageTagHello;
    crypto.getRandomValues(hello.hello.nonce);
    hello.hello.token = new TextEncoder().encode(token);
    const w = new Writer(txBuf);
    encodeHandshakeMessage(hello, w);
    ws.send(w.bytes().slice());
  };

  ws.onmessage = (ev) => {
    const data = new Uint8Array(ev.data as ArrayBuffer);
    if (!welcomed) {
      const hs = createHandshakeMessage();
      decodeHandshakeMessage(new Reader(data), hs);
      if (hs.tag === HandshakeMessageTagReject) {
        console.error(`rejected: ${hs.reject.reason}`);
        ws.close();
        return;
      }
      if (hs.tag === HandshakeMessageTagWelcome) {
        welcomed = true;
        console.log(`connected as peer ${hs.welcome.peer} (server ${hs.welcome.server})`);
      }
      return;
    }
    const res = client.handleTransportFrame(data);
    console.log(`[${res.kind}] epoch=${client.epoch} tick=${client.tick} entities=${client.entities.size}`);
    if (res.reply) {
      ws.send(client.wrapTransportFrame(seq++, res.reply, txBuf).slice());
    }
  };
}

function runDemo(): void {
  const scratch = new Uint8Array(1024);
  const w = new Writer(scratch);
  const msg = createReplicationMessage();
  const client = new ReplicationStreamClient();

  // Build a Full snapshot at tick 1.
  const hero = createEntityRecord();
  hero.id = 1;
  hero.fieldsMask = EntityRecordFieldKind | EntityRecordFieldX | EntityRecordFieldY | EntityRecordFieldProps;
  hero.kind = 10;
  hero.x = 100;
  hero.y = 50;
  hero.props = new Uint8Array([0x01, 0x02, 0x03]);

  msg.tag = ReplicationMessageTagFull;
  msg.version = ReplicationMessageVersion;
  msg.full = { epoch: 42, tick: 1, entities: [hero] };
  w.reset();
  encodeReplicationMessage(msg, w);
  const r1 = client.handlePacket(w.bytes().slice());
  console.log(`tick ${r1.tick} (${r1.kind}):`, Array.from(client.entities.values()));

  // Build a Delta snapshot at tick 2 moving hero.x and setting flags.
  const heroDelta = createEntityRecord();
  heroDelta.id = 1;
  heroDelta.fieldsMask = EntityRecordFieldX | EntityRecordFieldFlags;
  heroDelta.x = 112.5;
  heroDelta.flags = 0x04;

  msg.tag = ReplicationMessageTagDelta;
  msg.delta = { epoch: 42, tick: 2, baselineTick: 1, despawns: [], updates: [heroDelta] };
  w.reset();
  encodeReplicationMessage(msg, w);
  const r2 = client.handlePacket(w.bytes().slice());
  console.log(`tick ${r2.tick} (${r2.kind}):`, Array.from(client.entities.values()));
}

const args = process.argv.slice(2);
if (args.includes("--e2e")) {
  await runE2EBridge();
} else if (args.includes("--url")) {
  const url = args[args.indexOf("--url") + 1] ?? "ws://127.0.0.1:8080";
  const token = args.includes("--token") ? (args[args.indexOf("--token") + 1] ?? "") : "";
  await runWebSocketClient(url, token);
} else {
  runDemo();
}
