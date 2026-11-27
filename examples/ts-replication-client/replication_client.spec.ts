import { describe, expect, test } from "bun:test";
import {
  EntityRecordFieldFlags,
  EntityRecordFieldKind,
  EntityRecordFieldProps,
  EntityRecordFieldX,
  EntityRecordFieldY,
  Reader,
  ReplicationMessageTagAck,
  ReplicationMessageTagDelta,
  ReplicationMessageTagFull,
  ReplicationMessageTagFullPart,
  ReplicationMessageTagResync,
  ReplicationMessageVersion,
  Writer,
  createEntityRecord,
  createReplicationMessage,
  decodeReplicationMessage,
  encodeReplicationMessage,
  float32Bits,
} from "../../pkg/online/netplay/wire/ts/index.ts";
import { ReplicationStreamClient } from "./replication_client.ts";

describe("ReplicationStreamClient", () => {
  test("decodes Full, Delta (update + spawn + despawn), and split FullPart snapshots", () => {
    const client = new ReplicationStreamClient();
    const buf = new Uint8Array(2048);
    const w = new Writer(buf);
    const msg = createReplicationMessage();

    // Tick 1: Full snapshot with entities 1 and 2.
    const e1 = createEntityRecord();
    e1.id = 1;
    e1.fieldsMask = EntityRecordFieldKind | EntityRecordFieldX | EntityRecordFieldY | EntityRecordFieldProps;
    e1.kind = 1;
    e1.x = 10;
    e1.y = -0;
    e1.props = new Uint8Array([0xaa, 0xbb]);

    const e2 = createEntityRecord();
    e2.id = 2;
    e2.fieldsMask = EntityRecordFieldKind | EntityRecordFieldFlags;
    e2.kind = 2;
    e2.flags = 7;

    msg.tag = ReplicationMessageTagFull;
    msg.version = ReplicationMessageVersion;
    msg.full = { epoch: 100, tick: 1, entities: [e1, e2] };
    w.reset();
    encodeReplicationMessage(msg, w);

    const r1 = client.handlePacket(w.bytes().slice());
    expect(r1.kind).toBe("full");
    expect(client.epoch).toBe(100);
    expect(client.tick).toBe(1);
    expect(client.entities.size).toBe(2);
    expect(float32Bits(client.entities.get(1)!.y)).toBe(0x80000000);
    expect(r1.reply).toBeDefined();

    // Verify generated SnapshotAck decodes cleanly.
    const ack = createReplicationMessage();
    decodeReplicationMessage(new Reader(r1.reply!), ack);
    expect(ack.tag).toBe(ReplicationMessageTagAck);
    expect(ack.ack).toEqual({ epoch: 100, tick: 1 });

    // Tick 2: Delta updates entity 1, despawns entity 2, spawns entity 3.
    const e1Delta = createEntityRecord();
    e1Delta.id = 1;
    e1Delta.fieldsMask = EntityRecordFieldX;
    e1Delta.x = 42.5;

    const e3 = createEntityRecord();
    e3.id = 3;
    e3.fieldsMask = EntityRecordFieldKind | EntityRecordFieldY;
    e3.kind = 9;
    e3.y = 99;

    msg.tag = ReplicationMessageTagDelta;
    msg.delta = { epoch: 100, tick: 2, baselineTick: 1, despawns: [2], updates: [e1Delta, e3] };
    w.reset();
    encodeReplicationMessage(msg, w);

    const r2 = client.handlePacket(w.bytes().slice());
    expect(r2.kind).toBe("delta");
    expect(client.entities.has(2)).toBe(false);
    expect(client.entities.get(1)!.x).toBe(42.5);
    expect(client.entities.get(1)!.kind).toBe(1);
    expect(Array.from(client.entities.get(1)!.props)).toEqual([0xaa, 0xbb]);
    expect(client.entities.get(3)!.kind).toBe(9);
    expect(client.entities.get(3)!.y).toBe(99);

    // Mismatched baseline requests resync.
    msg.delta = { epoch: 100, tick: 5, baselineTick: 4, despawns: [], updates: [] };
    w.reset();
    encodeReplicationMessage(msg, w);
    const rResync = client.handlePacket(w.bytes().slice());
    expect(rResync.kind).toBe("resync_needed");
    const resyncMsg = createReplicationMessage();
    decodeReplicationMessage(new Reader(rResync.reply!), resyncMsg);
    expect(resyncMsg.tag).toBe(ReplicationMessageTagResync);
    expect(resyncMsg.resync.epoch).toBe(100);

    // Split FullPart in a new epoch (epoch 101).
    msg.tag = ReplicationMessageTagFullPart;
    msg.fullPart = { epoch: 101, tick: 1, index: 0, total: 2, entities: [e1] };
    w.reset();
    encodeReplicationMessage(msg, w);
    expect(client.handlePacket(w.bytes().slice()).kind).toBe("full_part_pending");

    msg.fullPart = { epoch: 101, tick: 1, index: 1, total: 2, entities: [e3] };
    w.reset();
    encodeReplicationMessage(msg, w);
    const rFullPart = client.handlePacket(w.bytes().slice());
    expect(rFullPart.kind).toBe("full_part_complete");
    expect(client.epoch).toBe(101);
    expect(client.entities.size).toBe(2);
    expect(client.entities.has(1)).toBe(true);
    expect(client.entities.has(3)).toBe(true);
  });
});
