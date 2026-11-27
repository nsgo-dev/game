import {
  Channel,
  EMPTY_BYTES,
  Reader,
  ReplicationMessageTagAck,
  ReplicationMessageTagDelta,
  ReplicationMessageTagFull,
  ReplicationMessageTagFullPart,
  ReplicationMessageTagResync,
  ReplicationMessageVersion,
  Writer,
  applyEntityRecord,
  createEntityRecord,
  createReplicationMessage,
  createTransportFrame,
  decodeReplicationMessage,
  decodeTransportFrame,
  encodeReplicationMessage,
  encodeTransportFrame,
  type EntityRecord,
  type ReplicationMessage,
  type TransportFrame,
} from "../../pkg/online/netplay/wire/ts/index.ts";

export interface StepResult {
  kind: "full" | "delta" | "full_part_pending" | "full_part_complete" | "resync_needed" | "ignored";
  epoch: number;
  tick: number;
  reply?: Uint8Array;
}

interface PendingParts {
  epoch: number;
  tick: number;
  total: number;
  received: number;
  parts: Array<EntityRecord[] | undefined>;
}

function cloneEntityRecord(src: EntityRecord): EntityRecord {
  return {
    id: src.id,
    fieldsMask: src.fieldsMask,
    kind: src.kind,
    x: src.x,
    y: src.y,
    vx: src.vx,
    vy: src.vy,
    rotation: src.rotation,
    flags: src.flags,
    props: src.props.byteLength === 0 ? EMPTY_BYTES : src.props.slice(),
  };
}

/**
 * ReplicationStreamClient decodes pkg/online/netplay replication v2 packets
 * (Full, Delta, and split FullPart snapshots) using generated TypeScript codecs
 * and produces SnapshotAck / ResyncRequest replies.
 */
export class ReplicationStreamClient {
  epoch = 0;
  hasEpoch = false;
  tick = 0;
  readonly entities = new Map<number, EntityRecord>();

  private readonly reader = new Reader(EMPTY_BYTES);
  private readonly msg: ReplicationMessage = createReplicationMessage();
  private readonly txBuf = new Uint8Array(64);
  private readonly txWriter = new Writer(this.txBuf);
  private readonly replyMsg: ReplicationMessage = createReplicationMessage();
  private readonly frame: TransportFrame = createTransportFrame();
  private pending: PendingParts | null = null;

  /**
   * Decodes one replication v2 payload (the body inside a TransportFrame).
   */
  handlePacket(packet: Uint8Array): StepResult {
    this.reader.reset(packet);
    decodeReplicationMessage(this.reader, this.msg);

    switch (this.msg.tag) {
      case ReplicationMessageTagFull: {
        const full = this.msg.full;
        this.applyFullWorld(full.epoch, full.tick, full.entities);
        return {
          kind: "full",
          epoch: this.epoch,
          tick: this.tick,
          reply: this.encodeAck(this.epoch, this.tick),
        };
      }

      case ReplicationMessageTagDelta: {
        const delta = this.msg.delta;
        if (!this.hasEpoch || delta.epoch !== this.epoch || delta.baselineTick !== this.tick) {
          return {
            kind: "resync_needed",
            epoch: delta.epoch,
            tick: delta.tick,
            reply: this.encodeResync(delta.epoch),
          };
        }
        for (const id of delta.despawns) {
          this.entities.delete(id);
        }
        for (const upd of delta.updates) {
          let ent = this.entities.get(upd.id);
          if (ent === undefined) {
            ent = createEntityRecord();
            ent.id = upd.id;
            this.entities.set(upd.id, ent);
          }
          applyEntityRecord(ent, upd);
        }
        this.tick = delta.tick;
        return {
          kind: "delta",
          epoch: this.epoch,
          tick: this.tick,
          reply: this.encodeAck(this.epoch, this.tick),
        };
      }

      case ReplicationMessageTagFullPart: {
        const part = this.msg.fullPart;
        if (part.total === 0 || part.index >= part.total) {
          return { kind: "ignored", epoch: this.epoch, tick: this.tick };
        }
        if (
          this.pending === null ||
          this.pending.epoch !== part.epoch ||
          this.pending.tick !== part.tick ||
          this.pending.total !== part.total
        ) {
          this.pending = {
            epoch: part.epoch,
            tick: part.tick,
            total: part.total,
            received: 0,
            parts: new Array(part.total),
          };
        }
        if (this.pending.parts[part.index] === undefined) {
          this.pending.parts[part.index] = part.entities.map(cloneEntityRecord);
          this.pending.received += 1;
        }
        if (this.pending.received < this.pending.total) {
          return { kind: "full_part_pending", epoch: part.epoch, tick: part.tick };
        }
        const assembled: EntityRecord[] = [];
        for (const chunk of this.pending.parts) {
          if (chunk !== undefined) {
            for (const e of chunk) assembled.push(e);
          }
        }
        this.pending = null;
        this.applyFullWorld(part.epoch, part.tick, assembled);
        return {
          kind: "full_part_complete",
          epoch: this.epoch,
          tick: this.tick,
          reply: this.encodeAck(this.epoch, this.tick),
        };
      }

      default:
        return { kind: "ignored", epoch: this.epoch, tick: this.tick };
    }
  }

  /**
   * Unwraps a pkg/online/netplay TransportFrame and feeds its payload into handlePacket.
   */
  handleTransportFrame(frameBytes: Uint8Array): StepResult {
    this.reader.reset(frameBytes);
    decodeTransportFrame(this.reader, this.frame);
    return this.handlePacket(this.frame.payload);
  }

  /**
   * Wraps a reply payload into a TransportFrame suitable for sending over WebSocketTransport.
   */
  wrapTransportFrame(seq: number, payload: Uint8Array, outBuf: Uint8Array): Uint8Array {
    this.frame.channel = Channel.Unreliable;
    this.frame.seq = seq;
    this.frame.payload = payload;
    const w = new Writer(outBuf);
    encodeTransportFrame(this.frame, w);
    return w.bytes();
  }

  private applyFullWorld(epoch: number, tick: number, incoming: EntityRecord[]): void {
    this.epoch = epoch;
    this.hasEpoch = true;
    this.tick = tick;
    this.pending = null;
    this.entities.clear();
    for (const src of incoming) {
      const dst = createEntityRecord();
      dst.id = src.id;
      applyEntityRecord(dst, src);
      this.entities.set(dst.id, dst);
    }
  }

  private encodeAck(epoch: number, tick: number): Uint8Array {
    this.replyMsg.tag = ReplicationMessageTagAck;
    this.replyMsg.version = ReplicationMessageVersion;
    this.replyMsg.ack.epoch = epoch;
    this.replyMsg.ack.tick = tick;
    this.txWriter.reset();
    encodeReplicationMessage(this.replyMsg, this.txWriter);
    return this.txWriter.bytes().slice();
  }

  private encodeResync(epoch: number): Uint8Array {
    this.replyMsg.tag = ReplicationMessageTagResync;
    this.replyMsg.version = ReplicationMessageVersion;
    this.replyMsg.resync.epoch = epoch;
    this.txWriter.reset();
    encodeReplicationMessage(this.replyMsg, this.txWriter);
    return this.txWriter.bytes().slice();
  }
}
