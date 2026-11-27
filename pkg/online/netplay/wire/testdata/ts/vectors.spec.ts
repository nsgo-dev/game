import { describe, expect, test } from "bun:test";
import { readdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import * as wire from "../../ts/index.ts";
import * as td from "./schemas/index.ts";

const __dirname = dirname(fileURLToPath(import.meta.url));
const vectorsDir = join(__dirname, "..", "vectors");

function hexToBytes(hex: string): Uint8Array {
  if (hex.length === 0) return wire.EMPTY_BYTES;
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

function asBigInt(v: unknown): bigint {
  if (typeof v === "bigint") return v;
  if (typeof v === "string") return BigInt(v);
  if (typeof v === "number") return BigInt(v);
  throw new Error(`cannot convert ${typeof v} (${String(v)}) to bigint`);
}

function asNum(v: unknown): number {
  if (typeof v === "number") return v;
  if (typeof v === "string") return Number(v);
  throw new Error(`cannot convert ${typeof v} (${String(v)}) to number`);
}

function asBytes(v: unknown): Uint8Array {
  return hexToBytes(String(v ?? ""));
}

type RawObj = Record<string, unknown>;

interface VectorCodec<T> {
  fromYAML(raw: RawObj): T;
  create(): T;
  encode(m: T, w: wire.Writer): void;
  decode(r: wire.Reader, m: T): void;
  assertEqual(want: T, got: T, label: string): void;
  diff?(base: T, cur: T): number | bigint;
  apply?(base: T, delta: T): void;
  setMask?(m: T, mask: number | bigint): void;
}

function assertF32Equal(want: number, got: number, label: string): void {
  const wb = wire.float32Bits(want);
  const gb = wire.float32Bits(got);
  if (wb !== gb) {
    throw new Error(`${label}: float32 bits mismatch: want 0x${wb.toString(16).padStart(8, "0")} (${want}), got 0x${gb.toString(16).padStart(8, "0")} (${got})`);
  }
}

function assertF64Equal(want: number, got: number, label: string): void {
  const wb = wire.float64Bits(want);
  const gb = wire.float64Bits(got);
  if (wb !== gb) {
    throw new Error(`${label}: float64 bits mismatch: want 0x${wb.toString(16).padStart(16, "0")} (${want}), got 0x${gb.toString(16).padStart(16, "0")} (${got})`);
  }
}

function assertBytesEq(want: Uint8Array, got: Uint8Array, label: string): void {
  if (!wire.bytesEqual(want, got)) {
    throw new Error(`${label}: bytes mismatch: want ${bytesToHex(want)}, got ${bytesToHex(got)}`);
  }
}

function fromYAMLEntityRecord(raw: RawObj): wire.EntityRecord {
  return {
    id: asNum(raw.id),
    fieldsMask: asNum(raw.fieldsMask),
    kind: asNum(raw.kind),
    x: asNum(raw.x),
    y: asNum(raw.y),
    vx: asNum(raw.vx),
    vy: asNum(raw.vy),
    rotation: asNum(raw.rotation),
    flags: asNum(raw.flags),
    props: asBytes(raw.props),
  };
}

function assertEntityRecordEqual(want: wire.EntityRecord, got: wire.EntityRecord, label: string): void {
  expect(got.id, `${label}.id`).toBe(want.id);
  expect(got.fieldsMask, `${label}.fieldsMask`).toBe(want.fieldsMask);
  expect(got.kind, `${label}.kind`).toBe(want.kind);
  assertF32Equal(want.x, got.x, `${label}.x`);
  assertF32Equal(want.y, got.y, `${label}.y`);
  assertF32Equal(want.vx, got.vx, `${label}.vx`);
  assertF32Equal(want.vy, got.vy, `${label}.vy`);
  assertF32Equal(want.rotation, got.rotation, `${label}.rotation`);
  expect(got.flags, `${label}.flags`).toBe(want.flags);
  assertBytesEq(want.props, got.props, `${label}.props`);
}

function fromYAMLInputFrame(raw: RawObj): wire.InputFrame {
  const axes = raw.axes as number[];
  return {
    tick: asNum(raw.tick),
    peer: asNum(raw.peer),
    bits: asBigInt(raw.bits),
    axes: [asNum(axes[0]), asNum(axes[1])],
  };
}

function assertInputFrameEqual(want: wire.InputFrame, got: wire.InputFrame, label: string): void {
  expect(got.tick, `${label}.tick`).toBe(want.tick);
  expect(got.peer, `${label}.peer`).toBe(want.peer);
  expect(got.bits, `${label}.bits`).toBe(want.bits);
  expect(got.axes, `${label}.axes`).toEqual(want.axes);
}

function fromYAMLMatchMember(raw: RawObj): wire.MatchMember {
  return {
    id: asNum(raw.id),
    from: asNum(raw.from),
    hasLeave: Boolean(raw.hasLeave),
    until: asNum(raw.until),
  };
}

function fromYAMLMixedEndian(raw: RawObj): td.MixedEndian {
  return {
    beU16: asNum(raw.beU16),
    leU16: asNum(raw.leU16),
    beU32: asNum(raw.beU32),
    leU32: asNum(raw.leU32),
    beU64: asBigInt(raw.beU64),
    leU64: asBigInt(raw.leU64),
    beI64: asBigInt(raw.beI64),
    leI64: asBigInt(raw.leI64),
    beF32: asNum(raw.beF32),
    leF32: asNum(raw.leF32),
    mode: asBigInt(raw.mode) as td.BigMode,
  };
}

function assertMixedEndianEqual(want: td.MixedEndian, got: td.MixedEndian, label: string): void {
  expect(got.beU16, `${label}.beU16`).toBe(want.beU16);
  expect(got.leU16, `${label}.leU16`).toBe(want.leU16);
  expect(got.beU32, `${label}.beU32`).toBe(want.beU32);
  expect(got.leU32, `${label}.leU32`).toBe(want.leU32);
  expect(got.beU64, `${label}.beU64`).toBe(want.beU64);
  expect(got.leU64, `${label}.leU64`).toBe(want.leU64);
  expect(got.beI64, `${label}.beI64`).toBe(want.beI64);
  expect(got.leI64, `${label}.leI64`).toBe(want.leI64);
  assertF32Equal(want.beF32, got.beF32, `${label}.beF32`);
  assertF32Equal(want.leF32, got.leF32, `${label}.leF32`);
  expect(got.mode, `${label}.mode`).toBe(want.mode);
}

function fromYAMLVarintEdge(raw: RawObj): td.VarintEdge {
  return {
    u8V: asNum(raw.u8V),
    u16V: asNum(raw.u16V),
    u32V: asNum(raw.u32V),
    u64V: asBigInt(raw.u64V),
    s8V: asNum(raw.s8V),
    s16V: asNum(raw.s16V),
    s32V: asNum(raw.s32V),
    s64V: asBigInt(raw.s64V),
  };
}

function assertVarintEdgeEqual(want: td.VarintEdge, got: td.VarintEdge, label: string): void {
  expect(got.u8V, `${label}.u8V`).toBe(want.u8V);
  expect(got.u16V, `${label}.u16V`).toBe(want.u16V);
  expect(got.u32V, `${label}.u32V`).toBe(want.u32V);
  expect(got.u64V, `${label}.u64V`).toBe(want.u64V);
  expect(got.s8V, `${label}.s8V`).toBe(want.s8V);
  expect(got.s16V, `${label}.s16V`).toBe(want.s16V);
  expect(got.s32V, `${label}.s32V`).toBe(want.s32V);
  expect(got.s64V, `${label}.s64V`).toBe(want.s64V);
}

function fromYAMLQuantizedPacket(raw: RawObj): td.QuantizedPacket {
  return {
    flag1: asNum(raw.flag1),
    tag3: asNum(raw.tag3),
    wide40: asBigInt(raw.wide40),
    q1Unit: asNum(raw.q1Unit),
    q12Angle: asNum(raw.q12Angle),
    q10Asym: asNum(raw.q10Asym),
    q8Le: asNum(raw.q8Le),
    q16Le: asNum(raw.q16Le),
    q32Le: asNum(raw.q32Le),
    fxI16: asNum(raw.fxI16),
    fxSvar: asNum(raw.fxSvar),
    fxU64: asNum(raw.fxU64),
  };
}

function assertQuantizedPacketEqual(want: td.QuantizedPacket, got: td.QuantizedPacket, label: string): void {
  expect(got.flag1, `${label}.flag1`).toBe(want.flag1);
  expect(got.tag3, `${label}.tag3`).toBe(want.tag3);
  expect(got.wide40, `${label}.wide40`).toBe(want.wide40);
  assertF32Equal(want.q1Unit, got.q1Unit, `${label}.q1Unit`);
  assertF32Equal(want.q12Angle, got.q12Angle, `${label}.q12Angle`);
  assertF32Equal(want.q10Asym, got.q10Asym, `${label}.q10Asym`);
  assertF32Equal(want.q8Le, got.q8Le, `${label}.q8Le`);
  assertF32Equal(want.q16Le, got.q16Le, `${label}.q16Le`);
  assertF32Equal(want.q32Le, got.q32Le, `${label}.q32Le`);
  assertF64Equal(want.fxI16, got.fxI16, `${label}.fxI16`);
  assertF64Equal(want.fxSvar, got.fxSvar, `${label}.fxSvar`);
  assertF64Equal(want.fxU64, got.fxU64, `${label}.fxU64`);
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const codecs: Record<string, VectorCodec<any>> = {
  TransportFrame: {
    fromYAML: (raw): wire.TransportFrame => ({
      channel: asNum(raw.channel) as wire.Channel,
      seq: asNum(raw.seq),
      payload: asBytes(raw.payload),
    }),
    create: wire.createTransportFrame,
    encode: wire.encodeTransportFrame,
    decode: wire.decodeTransportFrame,
    assertEqual: (want, got, label) => {
      expect(got.channel, `${label}.channel`).toBe(want.channel);
      expect(got.seq, `${label}.seq`).toBe(want.seq);
      assertBytesEq(want.payload, got.payload, `${label}.payload`);
    },
  },
  UDPPeerFrame: {
    fromYAML: (raw): wire.UDPPeerFrame => ({
      from: asNum(raw.from),
      channel: asNum(raw.channel) as wire.Channel,
      seq: asNum(raw.seq),
      payload: asBytes(raw.payload),
    }),
    create: wire.createUDPPeerFrame,
    encode: wire.encodeUDPPeerFrame,
    decode: wire.decodeUDPPeerFrame,
    assertEqual: (want, got, label) => {
      expect(got.from, `${label}.from`).toBe(want.from);
      expect(got.channel, `${label}.channel`).toBe(want.channel);
      expect(got.seq, `${label}.seq`).toBe(want.seq);
      assertBytesEq(want.payload, got.payload, `${label}.payload`);
    },
  },
  UDPSealedDatagram: {
    fromYAML: (raw): wire.UDPSealedDatagram => ({
      type: asNum(raw.type) as wire.UDPDatagramType,
      peer: asNum(raw.peer),
      seq: asNum(raw.seq),
      channel: asNum(raw.channel) as wire.Channel,
      sealed: asBytes(raw.sealed),
    }),
    create: wire.createUDPSealedDatagram,
    encode: wire.encodeUDPSealedDatagram,
    decode: wire.decodeUDPSealedDatagram,
    assertEqual: (want, got, label) => {
      expect(got.type, `${label}.type`).toBe(want.type);
      expect(got.peer, `${label}.peer`).toBe(want.peer);
      expect(got.seq, `${label}.seq`).toBe(want.seq);
      expect(got.channel, `${label}.channel`).toBe(want.channel);
      assertBytesEq(want.sealed, got.sealed, `${label}.sealed`);
    },
  },
  HandshakeMessage: {
    fromYAML: (raw): wire.HandshakeMessage => {
      const m = wire.createHandshakeMessage();
      m.tag = asNum(raw.tag);
      if (m.tag === wire.HandshakeMessageTagHello) {
        const h = raw.hello as RawObj;
        m.hello = {
          nonce: asBytes(h.nonce),
          resumePeer: asNum(h.resumePeer),
          resumeSecret: asBytes(h.resumeSecret),
          token: asBytes(h.token),
        };
      } else if (m.tag === wire.HandshakeMessageTagWelcome) {
        const w = raw.welcome as RawObj;
        m.welcome = {
          nonce: asBytes(w.nonce),
          peer: asNum(w.peer),
          server: asNum(w.server),
          resumed: Boolean(w.resumed),
          secret: asBytes(w.secret),
          key: asBytes(w.key),
        };
      } else if (m.tag === wire.HandshakeMessageTagReject) {
        const r = raw.reject as RawObj;
        m.reject = { nonce: asBytes(r.nonce), reason: String(r.reason ?? "") };
      }
      return m;
    },
    create: wire.createHandshakeMessage,
    encode: wire.encodeHandshakeMessage,
    decode: wire.decodeHandshakeMessage,
    assertEqual: (want, got, label) => {
      expect(got.tag, `${label}.tag`).toBe(want.tag);
      if (want.tag === wire.HandshakeMessageTagHello) {
        assertBytesEq(want.hello.nonce, got.hello.nonce, `${label}.hello.nonce`);
        expect(got.hello.resumePeer).toBe(want.hello.resumePeer);
        assertBytesEq(want.hello.resumeSecret, got.hello.resumeSecret, `${label}.hello.resumeSecret`);
        assertBytesEq(want.hello.token, got.hello.token, `${label}.hello.token`);
      } else if (want.tag === wire.HandshakeMessageTagWelcome) {
        assertBytesEq(want.welcome.nonce, got.welcome.nonce, `${label}.welcome.nonce`);
        expect(got.welcome.peer).toBe(want.welcome.peer);
        expect(got.welcome.server).toBe(want.welcome.server);
        expect(got.welcome.resumed).toBe(want.welcome.resumed);
        assertBytesEq(want.welcome.secret, got.welcome.secret, `${label}.welcome.secret`);
        assertBytesEq(want.welcome.key, got.welcome.key, `${label}.welcome.key`);
      } else {
        assertBytesEq(want.reject.nonce, got.reject.nonce, `${label}.reject.nonce`);
        expect(got.reject.reason).toBe(want.reject.reason);
      }
    },
  },
  RelayFrame: {
    fromYAML: (raw): wire.RelayFrame => ({ peer: asNum(raw.peer), body: asBytes(raw.body) }),
    create: wire.createRelayFrame,
    encode: wire.encodeRelayFrame,
    decode: wire.decodeRelayFrame,
    assertEqual: (want, got, label) => {
      expect(got.peer, `${label}.peer`).toBe(want.peer);
      assertBytesEq(want.body, got.body, `${label}.body`);
    },
  },
  RelayMembers: {
    fromYAML: (raw): wire.RelayMembers => ({
      epoch: asNum(raw.epoch),
      peers: ((raw.peers as unknown[]) ?? []).map(asNum),
    }),
    create: wire.createRelayMembers,
    encode: wire.encodeRelayMembers,
    decode: wire.decodeRelayMembers,
    assertEqual: (want, got, label) => {
      expect(got.epoch, `${label}.epoch`).toBe(want.epoch);
      expect(got.peers, `${label}.peers`).toEqual(want.peers);
    },
  },
  RelayBody: {
    fromYAML: (raw): wire.RelayBody => {
      const m = wire.createRelayBody();
      m.tag = asNum(raw.tag);
      if (m.tag === wire.RelayBodyTagUnreliable) {
        const u = raw.unreliable as RawObj;
        m.unreliable = { seq: asNum(u.seq), payload: asBytes(u.payload) };
      } else if (m.tag === wire.RelayBodyTagReliable) {
        const r = raw.reliable as RawObj;
        m.reliable = { seq: asNum(r.seq), payload: asBytes(r.payload) };
      } else if (m.tag === wire.RelayBodyTagAck) {
        const a = raw.ack as RawObj;
        m.ack = { next: asNum(a.next), mask: asBigInt(a.mask) };
      }
      return m;
    },
    create: wire.createRelayBody,
    encode: wire.encodeRelayBody,
    decode: wire.decodeRelayBody,
    assertEqual: (want, got, label) => {
      expect(got.tag, `${label}.tag`).toBe(want.tag);
      if (want.tag === wire.RelayBodyTagUnreliable) {
        expect(got.unreliable.seq).toBe(want.unreliable.seq);
        assertBytesEq(want.unreliable.payload, got.unreliable.payload, `${label}.unreliable.payload`);
      } else if (want.tag === wire.RelayBodyTagReliable) {
        expect(got.reliable.seq).toBe(want.reliable.seq);
        assertBytesEq(want.reliable.payload, got.reliable.payload, `${label}.reliable.payload`);
      } else {
        expect(got.ack.next).toBe(want.ack.next);
        expect(got.ack.mask).toBe(want.ack.mask);
      }
    },
  },
  EntityRecord: {
    fromYAML: fromYAMLEntityRecord,
    create: wire.createEntityRecord,
    encode: wire.encodeEntityRecord,
    decode: wire.decodeEntityRecord,
    assertEqual: assertEntityRecordEqual,
    diff: wire.diffEntityRecord,
    apply: wire.applyEntityRecord,
    setMask: (m, mask) => { m.fieldsMask = Number(mask); },
  },
  ReplicationMessage: {
    fromYAML: (raw): wire.ReplicationMessage => {
      const m = wire.createReplicationMessage();
      m.tag = asNum(raw.tag);
      m.version = asNum(raw.version);
      if (m.tag === wire.ReplicationMessageTagFull) {
        const sub = raw.full as RawObj;
        m.full = {
          epoch: asNum(sub.epoch),
          tick: asNum(sub.tick),
          entities: ((sub.entities as RawObj[]) ?? []).map(fromYAMLEntityRecord),
        };
      } else if (m.tag === wire.ReplicationMessageTagDelta) {
        const sub = raw.delta as RawObj;
        m.delta = {
          epoch: asNum(sub.epoch),
          tick: asNum(sub.tick),
          baselineTick: asNum(sub.baselineTick),
          despawns: ((sub.despawns as unknown[]) ?? []).map(asNum),
          updates: ((sub.updates as RawObj[]) ?? []).map(fromYAMLEntityRecord),
        };
      } else if (m.tag === wire.ReplicationMessageTagAck) {
        const sub = raw.ack as RawObj;
        m.ack = { epoch: asNum(sub.epoch), tick: asNum(sub.tick) };
      } else if (m.tag === wire.ReplicationMessageTagResync) {
        const sub = raw.resync as RawObj;
        m.resync = { epoch: asNum(sub.epoch) };
      } else if (m.tag === wire.ReplicationMessageTagFullPart) {
        const sub = raw.fullPart as RawObj;
        m.fullPart = {
          epoch: asNum(sub.epoch),
          tick: asNum(sub.tick),
          index: asNum(sub.index),
          total: asNum(sub.total),
          entities: ((sub.entities as RawObj[]) ?? []).map(fromYAMLEntityRecord),
        };
      }
      return m;
    },
    create: wire.createReplicationMessage,
    encode: wire.encodeReplicationMessage,
    decode: wire.decodeReplicationMessage,
    assertEqual: (want, got, label) => {
      expect(got.tag, `${label}.tag`).toBe(want.tag);
      expect(got.version, `${label}.version`).toBe(want.version);
      if (want.tag === wire.ReplicationMessageTagFull) {
        expect(got.full.epoch).toBe(want.full.epoch);
        expect(got.full.tick).toBe(want.full.tick);
        expect(got.full.entities.length).toBe(want.full.entities.length);
        for (let i = 0; i < want.full.entities.length; i++) {
          assertEntityRecordEqual(want.full.entities[i]!, got.full.entities[i]!, `${label}.full.entities[${i}]`);
        }
      } else if (want.tag === wire.ReplicationMessageTagDelta) {
        expect(got.delta.epoch).toBe(want.delta.epoch);
        expect(got.delta.tick).toBe(want.delta.tick);
        expect(got.delta.baselineTick).toBe(want.delta.baselineTick);
        expect(got.delta.despawns).toEqual(want.delta.despawns);
        expect(got.delta.updates.length).toBe(want.delta.updates.length);
        for (let i = 0; i < want.delta.updates.length; i++) {
          assertEntityRecordEqual(want.delta.updates[i]!, got.delta.updates[i]!, `${label}.delta.updates[${i}]`);
        }
      } else if (want.tag === wire.ReplicationMessageTagAck) {
        expect(got.ack).toEqual(want.ack);
      } else if (want.tag === wire.ReplicationMessageTagResync) {
        expect(got.resync).toEqual(want.resync);
      } else if (want.tag === wire.ReplicationMessageTagFullPart) {
        expect(got.fullPart.epoch).toBe(want.fullPart.epoch);
        expect(got.fullPart.tick).toBe(want.fullPart.tick);
        expect(got.fullPart.index).toBe(want.fullPart.index);
        expect(got.fullPart.total).toBe(want.fullPart.total);
        expect(got.fullPart.entities.length).toBe(want.fullPart.entities.length);
        for (let i = 0; i < want.fullPart.entities.length; i++) {
          assertEntityRecordEqual(want.fullPart.entities[i]!, got.fullPart.entities[i]!, `${label}.fullPart.entities[${i}]`);
        }
      }
    },
  },
  InputFrame: {
    fromYAML: fromYAMLInputFrame,
    create: wire.createInputFrame,
    encode: wire.encodeInputFrame,
    decode: wire.decodeInputFrame,
    assertEqual: assertInputFrameEqual,
  },
  RollbackMessage: {
    fromYAML: (raw): wire.RollbackMessage => {
      const m = wire.createRollbackMessage();
      m.tag = asNum(raw.tag);
      if (m.tag === wire.RollbackMessageTagSingleInput) {
        const sub = raw.singleInput as RawObj;
        m.singleInput = { input: fromYAMLInputFrame(sub.input as RawObj) };
      } else if (m.tag === wire.RollbackMessageTagInputBundle) {
        const sub = raw.inputBundle as RawObj;
        m.inputBundle = { inputs: ((sub.inputs as RawObj[]) ?? []).map(fromYAMLInputFrame) };
      } else if (m.tag === wire.RollbackMessageTagChecksum) {
        const sub = raw.checksum as RawObj;
        m.checksum = { tick: asNum(sub.tick), peer: asNum(sub.peer), checksum: asBigInt(sub.checksum) };
      }
      return m;
    },
    create: wire.createRollbackMessage,
    encode: wire.encodeRollbackMessage,
    decode: wire.decodeRollbackMessage,
    assertEqual: (want, got, label) => {
      expect(got.tag).toBe(want.tag);
      if (want.tag === wire.RollbackMessageTagSingleInput) {
        assertInputFrameEqual(want.singleInput.input, got.singleInput.input, `${label}.singleInput`);
      } else if (want.tag === wire.RollbackMessageTagInputBundle) {
        expect(got.inputBundle.inputs.length).toBe(want.inputBundle.inputs.length);
        for (let i = 0; i < want.inputBundle.inputs.length; i++) {
          assertInputFrameEqual(want.inputBundle.inputs[i]!, got.inputBundle.inputs[i]!, `${label}.inputBundle[${i}]`);
        }
      } else {
        expect(got.checksum).toEqual(want.checksum);
      }
    },
  },
  MatchMember: {
    fromYAML: fromYAMLMatchMember,
    create: wire.createMatchMember,
    encode: wire.encodeMatchMember,
    decode: wire.decodeMatchMember,
    assertEqual: (want, got, label) => {
      expect(got, label).toEqual(want);
    },
  },
  JoinState: {
    fromYAML: (raw): wire.JoinState => ({
      tick: asNum(raw.tick),
      members: ((raw.members as RawObj[]) ?? []).map(fromYAMLMatchMember),
      inputs: ((raw.inputs as RawObj[]) ?? []).map(fromYAMLInputFrame),
      state: asBytes(raw.state),
    }),
    create: wire.createJoinState,
    encode: wire.encodeJoinState,
    decode: wire.decodeJoinState,
    assertEqual: (want, got, label) => {
      expect(got.tick).toBe(want.tick);
      expect(got.members).toEqual(want.members);
      expect(got.inputs.length).toBe(want.inputs.length);
      for (let i = 0; i < want.inputs.length; i++) {
        assertInputFrameEqual(want.inputs[i]!, got.inputs[i]!, `${label}.inputs[${i}]`);
      }
      assertBytesEq(want.state, got.state, `${label}.state`);
    },
  },
  MembershipMessage: {
    fromYAML: (raw): wire.MembershipMessage => {
      const m = wire.createMembershipMessage();
      m.tag = asNum(raw.tag);
      if (m.tag === wire.MembershipMessageTagChange) {
        const sub = raw.change as RawObj;
        m.change = {
          kind: asNum(sub.kind) as wire.MemberChangeKind,
          peer: asNum(sub.peer),
          tick: asNum(sub.tick),
          inputs: ((sub.inputs as RawObj[]) ?? []).map(fromYAMLInputFrame),
        };
      } else if (m.tag === wire.MembershipMessageTagJoinStatePart) {
        const sub = raw.joinStatePart as RawObj;
        m.joinStatePart = {
          part: asNum(sub.part),
          parts: asNum(sub.parts),
          data: asBytes(sub.data),
        };
      }
      return m;
    },
    create: wire.createMembershipMessage,
    encode: wire.encodeMembershipMessage,
    decode: wire.decodeMembershipMessage,
    assertEqual: (want, got, label) => {
      expect(got.tag).toBe(want.tag);
      if (want.tag === wire.MembershipMessageTagChange) {
        expect(got.change.kind).toBe(want.change.kind);
        expect(got.change.peer).toBe(want.change.peer);
        expect(got.change.tick).toBe(want.change.tick);
        expect(got.change.inputs.length).toBe(want.change.inputs.length);
        for (let i = 0; i < want.change.inputs.length; i++) {
          assertInputFrameEqual(want.change.inputs[i]!, got.change.inputs[i]!, `${label}.change.inputs[${i}]`);
        }
      } else {
        expect(got.joinStatePart.part).toBe(want.joinStatePart.part);
        expect(got.joinStatePart.parts).toBe(want.joinStatePart.parts);
        assertBytesEq(want.joinStatePart.data, got.joinStatePart.data, `${label}.joinStatePart.data`);
      }
    },
  },
  FighterProps: {
    fromYAML: (raw): td.FighterProps => ({
      team: asNum(raw.team),
      combo: asNum(raw.combo),
      aimX: asNum(raw.aimX),
      aimY: asNum(raw.aimY),
      stance: asNum(raw.stance) as td.Stance,
      hpDelta: asNum(raw.hpDelta),
      stamina: asNum(raw.stamina),
      stateMask: asNum(raw.stateMask),
      shield: asNum(raw.shield),
      tag: String(raw.tag ?? ""),
      emote: asNum(raw.emote),
    }),
    create: td.createFighterProps,
    encode: td.encodeFighterProps,
    decode: td.decodeFighterProps,
    assertEqual: (want, got, label) => {
      expect(got.team).toBe(want.team);
      expect(got.combo).toBe(want.combo);
      assertF32Equal(want.aimX, got.aimX, `${label}.aimX`);
      assertF32Equal(want.aimY, got.aimY, `${label}.aimY`);
      expect(got.stance).toBe(want.stance);
      expect(got.hpDelta).toBe(want.hpDelta);
      assertF64Equal(want.stamina, got.stamina, `${label}.stamina`);
      expect(got.stateMask).toBe(want.stateMask);
      assertF32Equal(want.shield, got.shield, `${label}.shield`);
      expect(got.tag).toBe(want.tag);
      expect(got.emote).toBe(want.emote);
    },
    diff: td.diffFighterProps,
    apply: td.applyFighterProps,
    setMask: (m, mask) => { m.stateMask = Number(mask); },
  },
  MixedEndian: {
    fromYAML: fromYAMLMixedEndian,
    create: td.createMixedEndian,
    encode: td.encodeMixedEndian,
    decode: td.decodeMixedEndian,
    assertEqual: assertMixedEndianEqual,
  },
  VarintEdge: {
    fromYAML: fromYAMLVarintEdge,
    create: td.createVarintEdge,
    encode: td.encodeVarintEdge,
    decode: td.decodeVarintEdge,
    assertEqual: assertVarintEdgeEqual,
  },
  QuantizedPacket: {
    fromYAML: fromYAMLQuantizedPacket,
    create: td.createQuantizedPacket,
    encode: td.encodeQuantizedPacket,
    decode: td.decodeQuantizedPacket,
    assertEqual: assertQuantizedPacketEqual,
  },
  BytesBoundary: {
    fromYAML: (raw): td.BytesBoundary => ({
      small: asBytes(raw.small),
      medium: asBytes(raw.medium),
      label: String(raw.label ?? ""),
      tail: asBytes(raw.tail),
    }),
    create: td.createBytesBoundary,
    encode: td.encodeBytesBoundary,
    decode: td.decodeBytesBoundary,
    assertEqual: (want, got, label) => {
      assertBytesEq(want.small, got.small, `${label}.small`);
      assertBytesEq(want.medium, got.medium, `${label}.medium`);
      expect(got.label, `${label}.label`).toBe(want.label);
      assertBytesEq(want.tail, got.tail, `${label}.tail`);
    },
  },
  DeltaEdge: {
    fromYAML: (raw): td.DeltaEdge => ({
      seq: asNum(raw.seq),
      bodyMask: asNum(raw.bodyMask),
      posX: asNum(raw.posX),
      aim: asNum(raw.aim),
      energy: asNum(raw.energy),
      hash: asBigInt(raw.hash),
      blob: asBytes(raw.blob),
    }),
    create: td.createDeltaEdge,
    encode: td.encodeDeltaEdge,
    decode: td.decodeDeltaEdge,
    assertEqual: (want, got, label) => {
      expect(got.seq).toBe(want.seq);
      expect(got.bodyMask).toBe(want.bodyMask);
      assertF32Equal(want.posX, got.posX, `${label}.posX`);
      assertF32Equal(want.aim, got.aim, `${label}.aim`);
      assertF64Equal(want.energy, got.energy, `${label}.energy`);
      expect(got.hash).toBe(want.hash);
      assertBytesEq(want.blob, got.blob, `${label}.blob`);
    },
    diff: td.diffDeltaEdge,
    apply: td.applyDeltaEdge,
    setMask: (m, mask) => { m.bodyMask = Number(mask); },
  },
  EdgeEnvelope: {
    fromYAML: (raw): td.EdgeEnvelope => {
      const m = td.createEdgeEnvelope();
      m.tag = asBigInt(raw.tag);
      if (m.tag === td.EdgeEnvelopeTagMixed) {
        m.mixed = fromYAMLMixedEndian(raw.mixed as RawObj);
      } else if (m.tag === td.EdgeEnvelopeTagVarint) {
        m.varint = fromYAMLVarintEdge(raw.varint as RawObj);
      } else if (m.tag === td.EdgeEnvelopeTagQuantized) {
        m.quantized = fromYAMLQuantizedPacket(raw.quantized as RawObj);
      }
      return m;
    },
    create: td.createEdgeEnvelope,
    encode: td.encodeEdgeEnvelope,
    decode: td.decodeEdgeEnvelope,
    assertEqual: (want, got, label) => {
      expect(got.tag).toBe(want.tag);
      if (want.tag === td.EdgeEnvelopeTagMixed) {
        assertMixedEndianEqual(want.mixed, got.mixed, `${label}.mixed`);
      } else if (want.tag === td.EdgeEnvelopeTagVarint) {
        assertVarintEdgeEqual(want.varint, got.varint, `${label}.varint`);
      } else {
        assertQuantizedPacketEqual(want.quantized, got.quantized, `${label}.quantized`);
      }
    },
  },
};

describe("cross-language golden vectors", () => {
  test("quantize.yaml matches Go pure-integer qfloat and fixed bit-for-bit", () => {
    const doc = Bun.YAML.parse(readFileSync(join(vectorsDir, "quantize.yaml"), "utf8")) as {
      version: number;
      qfloat: Array<{ name: string; vBits: string; min: number; max: number; bits: number; wantQ: number; wantDeqBits: string }>;
      fixedSigned: Array<{ name: string; vBits: string; scale: string; minInt: string; maxInt: string; wantQ: string; wantDeqBits: string }>;
      fixedUnsigned: Array<{ name: string; vBits: string; scale: string; maxUint: string; wantQ: string; wantDeqBits: string }>;
    };
    expect(doc.version).toBe(1);

    for (const tc of doc.qfloat) {
      const vBits = parseInt(tc.vBits.replace(/^0x/, ""), 16) >>> 0;
      const wantDeqBits = parseInt(tc.wantDeqBits.replace(/^0x/, ""), 16) >>> 0;
      const v = wire.float32FromBits(vBits);
      const gotQ = wire.quantizeFloat(v, tc.min, tc.max, tc.bits);
      expect(gotQ, `qfloat ${tc.name} quantize`).toBe(tc.wantQ);
      const gotDeq = wire.dequantizeFloat(gotQ, tc.min, tc.max, tc.bits);
      expect(wire.float32Bits(gotDeq), `qfloat ${tc.name} dequantize`).toBe(wantDeqBits);
    }

    for (const tc of doc.fixedSigned) {
      const vBits = BigInt(tc.vBits);
      const wantDeqBits = BigInt(tc.wantDeqBits);
      const scale = BigInt(tc.scale);
      const gotQ = wire.quantizeFixed(wire.float64FromBits(vBits), scale, BigInt(tc.minInt), BigInt(tc.maxInt));
      expect(gotQ, `fixedSigned ${tc.name} quantize`).toBe(BigInt(tc.wantQ));
      const gotDeq = wire.dequantizeFixed(gotQ, scale);
      expect(wire.float64Bits(gotDeq), `fixedSigned ${tc.name} dequantize`).toBe(wantDeqBits);
    }

    for (const tc of doc.fixedUnsigned) {
      const vBits = BigInt(tc.vBits);
      const wantDeqBits = BigInt(tc.wantDeqBits);
      const scale = BigInt(tc.scale);
      const gotQ = wire.quantizeFixedUint(wire.float64FromBits(vBits), scale, BigInt(tc.maxUint));
      expect(gotQ, `fixedUnsigned ${tc.name} quantize`).toBe(BigInt(tc.wantQ));
      const gotDeq = wire.dequantizeFixedUint(gotQ, scale);
      expect(wire.float64Bits(gotDeq), `fixedUnsigned ${tc.name} dequantize`).toBe(wantDeqBits);
    }
  });

  const files = readdirSync(vectorsDir)
    .filter((f) => f.endsWith(".yaml") && f !== "quantize.yaml")
    .sort();

  expect(files.length).toBeGreaterThanOrEqual(9);

  // Reusable scratch buffer large enough for max-length 65535-byte vectors.
  const scratch = new Uint8Array(128 * 1024);

  for (const file of files) {
    test(file, () => {
      const doc = Bun.YAML.parse(readFileSync(join(vectorsDir, file), "utf8")) as {
        version: number;
        schema: string;
        vectors?: Array<{ name: string; type: string; hex: string; value: RawObj; decoded?: RawObj }>;
        deltas?: Array<{ name: string; type: string; base: RawObj; cur: RawObj; diffMask: number | string; deltaHex: string; expected: RawObj }>;
        invalid?: Array<{ name: string; type: string; hex: string; error: string }>;
      };
      expect(doc.version).toBe(1);

      const writer = new wire.Writer(scratch);
      const reader = new wire.Reader(wire.EMPTY_BYTES);

      for (const v of doc.vectors ?? []) {
        const c = codecs[v.type];
        expect(c, `codec for ${v.type}`).toBeDefined();
        const input = c!.fromYAML(v.value);
        const wantHex = String(v.hex);
        const wantBytes = hexToBytes(wantHex);

        // 1. Encode matches Go hex byte-for-byte.
        writer.reset();
        c!.encode(input, writer);
        const gotHex = bytesToHex(writer.bytes());
        expect(gotHex, `${file}:${v.name} encode`).toBe(wantHex);

        // 2. Decode into caller-owned reusable object matches bit-for-bit and consumes all bytes.
        const decoded = c!.create();
        reader.reset(wantBytes);
        c!.decode(reader, decoded);
        expect(reader.remaining(), `${file}:${v.name} trailing bytes`).toBe(0);

        const wantDecoded = c!.fromYAML(v.decoded ?? v.value);
        c!.assertEqual(wantDecoded, decoded, `${file}:${v.name} decode`);

        // 3. Re-encoding decoded object produces the exact same bytes.
        writer.reset();
        c!.encode(decoded, writer);
        expect(bytesToHex(writer.bytes()), `${file}:${v.name} re-encode`).toBe(wantHex);
      }

      for (const d of doc.deltas ?? []) {
        const c = codecs[d.type];
        expect(c?.diff && c?.apply && c?.setMask, `delta codec for ${d.type}`).toBeTruthy();

        const base = c!.fromYAML(d.base);
        const cur = c!.fromYAML(d.cur);
        const expected = c!.fromYAML(d.expected);
        const wantMask = typeof d.diffMask === "string" ? BigInt(d.diffMask) : d.diffMask;

        const gotMask = c!.diff!(base, cur);
        expect(gotMask, `${file}:delta:${d.name} diffMask`).toBe(wantMask);

        c!.setMask!(cur, gotMask);
        writer.reset();
        c!.encode(cur, writer);
        expect(bytesToHex(writer.bytes()), `${file}:delta:${d.name} deltaHex`).toBe(String(d.deltaHex));

        const decodedDelta = c!.create();
        reader.reset(hexToBytes(String(d.deltaHex)));
        c!.decode(reader, decodedDelta);
        expect(reader.remaining(), `${file}:delta:${d.name} trailing`).toBe(0);

        c!.apply!(base, decodedDelta);
        c!.assertEqual(expected, base, `${file}:delta:${d.name} apply`);
      }

      for (const inv of doc.invalid ?? []) {
        const c = codecs[inv.type];
        expect(c, `codec for ${inv.type}`).toBeDefined();
        const raw = hexToBytes(String(inv.hex));
        const target = c!.create();
        reader.reset(raw);
        let caught: unknown;
        try {
          c!.decode(reader, target);
        } catch (err) {
          caught = err;
        }
        expect(caught, `${file}:invalid:${inv.name} should throw`).toBeInstanceOf(Error);
        expect((caught as { code?: string }).code, `${file}:invalid:${inv.name} error code`).toBe(inv.error);
      }
    });
  }
});
