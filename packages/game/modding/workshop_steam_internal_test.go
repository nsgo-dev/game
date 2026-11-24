//go:build (darwin && !ios) || (linux && !android) || windows

package modding

import (
	"encoding/binary"
	"slices"
	"testing"
	"time"
)

func TestSteamCallbackDecodersUnixAndWindowsPacking(t *testing.T) {
	for _, pack := range []int{4, 8} {
		t.Run("CreateItemResult", func(t *testing.T) {
			var buf [32]byte
			binary.LittleEndian.PutUint32(buf[0:4], uint32(steamResultOK))
			idOff := alignOffset(4, pack)
			binary.LittleEndian.PutUint64(buf[idOff:idOff+8], 9876543210)
			buf[idOff+8] = 1

			eRes, id, needsLegal := decodeCreateItemResult(buf[:], pack)
			if eRes != steamResultOK || id != 9876543210 || !needsLegal {
				t.Fatalf("pack=%d got (%d, %d, %v)", pack, eRes, id, needsLegal)
			}
		})

		t.Run("SubmitItemUpdateResult", func(t *testing.T) {
			var buf [32]byte
			binary.LittleEndian.PutUint32(buf[0:4], uint32(steamResultOK))
			buf[4] = 1
			idOff := alignOffset(5, pack)
			binary.LittleEndian.PutUint64(buf[idOff:idOff+8], 424242)

			eRes, needsLegal, id := decodeSubmitItemUpdateResult(buf[:], pack)
			if eRes != steamResultOK || !needsLegal || id != 424242 {
				t.Fatalf("pack=%d got (%d, %v, %d)", pack, eRes, needsLegal, id)
			}
		})

		t.Run("SubscribeResult", func(t *testing.T) {
			var buf [32]byte
			binary.LittleEndian.PutUint32(buf[0:4], uint32(steamResultOK))
			idOff := alignOffset(4, pack)
			binary.LittleEndian.PutUint64(buf[idOff:idOff+8], 555666)

			eRes, id := decodeSubscribeResult(buf[:], pack)
			if eRes != steamResultOK || id != 555666 {
				t.Fatalf("pack=%d got (%d, %d)", pack, eRes, id)
			}
		})

		t.Run("UGCDetails", func(t *testing.T) {
			buf := make([]byte, steamUGCDetailsBufSize)
			binary.LittleEndian.PutUint64(buf[0:8], 777888)
			binary.LittleEndian.PutUint32(buf[8:12], uint32(steamResultOK))
			binary.LittleEndian.PutUint32(buf[20:24], 480)
			copy(buf[24:], "Boss Rush\x00")
			copy(buf[153:], "Custom wave mod\x00")

			off := alignOffset(153+8000, pack)
			binary.LittleEndian.PutUint64(buf[off:off+8], 76561198000000001)
			off += 8
			binary.LittleEndian.PutUint32(buf[off:off+4], 1700000000)
			off += 4
			binary.LittleEndian.PutUint32(buf[off:off+4], 1700003600)
			off += 4
			off += 4                                         // added to user list
			binary.LittleEndian.PutUint32(buf[off:off+4], 3) // unlisted
			off += 4
			off += 3 // flags
			copy(buf[off:], "bosses, gameplay\x00")

			item, ok := decodeUGCDetails(buf, pack)
			if !ok {
				t.Fatalf("pack=%d decodeUGCDetails returned false", pack)
			}
			if item.ID != 777888 || item.AppID != 480 || item.Title != "Boss Rush" || item.Description != "Custom wave mod" {
				t.Fatalf("pack=%d unexpected item header: %+v", pack, item)
			}
			if item.OwnerID != 76561198000000001 || item.Visibility != VisibilityUnlisted {
				t.Fatalf("pack=%d unexpected owner/vis: %+v", pack, item)
			}
			if !item.UpdatedAt.Equal(time.Unix(1700003600, 0).UTC()) {
				t.Fatalf("pack=%d UpdatedAt = %v", pack, item.UpdatedAt)
			}
			if !slices.Equal(item.Tags, []string{"bosses", "gameplay"}) {
				t.Fatalf("pack=%d Tags = %v", pack, item.Tags)
			}
		})
	}

	var completed [32]byte
	binary.LittleEndian.PutUint64(completed[0:8], 99)
	binary.LittleEndian.PutUint32(completed[8:12], uint32(steamResultOK))
	binary.LittleEndian.PutUint32(completed[12:16], 12)
	eRes, num := decodeQueryCompleted(completed[:])
	if eRes != steamResultOK || num != 12 {
		t.Fatalf("decodeQueryCompleted = (%d, %d), want (1, 12)", eRes, num)
	}
}
