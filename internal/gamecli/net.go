package gamecli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/nsgo-dev/game/pkg/online/netplay"
	"github.com/nsgo-dev/game/pkg/online/netplay/wire"
	"go.yaml.in/yaml/v3"
)

var netVerbs = []string{"decode", "stats"}

// netCommand decodes pkg/online/netplay packet captures using wire schemas or summarizes capture traffic statistics.
var netCommand = &command{
	name:    "net",
	args:    "decode [flags] <capture.jsonl | -> | stats [flags] <capture.jsonl | ->",
	summary: "Decode packet captures layer by layer using wire schemas, or print traffic statistics per message type and peer",
	define:  defineNet,
	complete: map[string]completion{
		"schema": filePaths,
		"format": oneOf("yaml", "json", "text"),
		"":       oneOf(netVerbs...),
	},
}

func defineNet(a *app, fs *flag.FlagSet) func() error {
	schemaPath := fs.String("schema", "", "directory or *.wire.yaml file of game-defined wire schemas (for example, custom EntityState.Props)")
	format := fs.String("format", "", "output format (decode: yaml or json, default yaml; stats: text, yaml, or json, default text)")
	resolveDeltas := fs.Bool("resolve-deltas", false, "track per-peer replication baselines so SnapshotDelta frames also print full resulting entity states (decode only)")

	return func() error {
		if fs.NArg() == 0 {
			return usagef("expected one of %s", strings.Join(netVerbs, ", "))
		}
		verb, rest := fs.Arg(0), fs.Args()[1:]

		// Also accept flags placed after the verb (for example `game net decode --format json --resolve-deltas cap.jsonl`).
		sub := flag.NewFlagSet("game net "+verb, flag.ContinueOnError)
		sub.SetOutput(a.stderr)
		subSchema := sub.String("schema", *schemaPath, "")
		subFormat := sub.String("format", *format, "")
		subResolve := sub.Bool("resolve-deltas", *resolveDeltas, "")
		if err := sub.Parse(rest); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				fs.Usage()
				return nil
			}
			return usagef("%v", err)
		}
		*schemaPath = *subSchema
		*format = *subFormat
		*resolveDeltas = *subResolve
		args := sub.Args()

		switch verb {
		case "decode":
			outFmt := *format
			if outFmt == "" {
				outFmt = "yaml"
			}
			if outFmt != "yaml" && outFmt != "json" {
				return usagef("-format %q must be yaml or json for decode", outFmt)
			}
			if len(args) != 1 {
				return usagef("decode takes one capture file path or - for stdin")
			}
			return runNetDecode(a, args[0], *schemaPath, outFmt, *resolveDeltas)

		case "stats":
			if *resolveDeltas {
				return usagef("-resolve-deltas is for decode")
			}
			outFmt := *format
			if outFmt == "" {
				outFmt = "text"
			}
			if outFmt != "text" && outFmt != "yaml" && outFmt != "json" {
				return usagef("-format %q must be text, yaml, or json for stats", outFmt)
			}
			if len(args) != 1 {
				return usagef("stats takes one capture file path or - for stdin")
			}
			return runNetStats(a, args[0], *schemaPath, outFmt)

		default:
			return usagef("unknown verb %q; use %s", verb, strings.Join(netVerbs, ", "))
		}
	}
}

func buildWireInspector(schemaPath string, resolveDeltas bool) (*wire.Inspector, error) {
	var custom []*wire.Schema
	if schemaPath != "" {
		var err error
		custom, err = wire.LoadSchemaPath(schemaPath)
		if err != nil {
			return nil, err
		}
	}
	reg, err := wire.NewRegistry(custom...)
	if err != nil {
		return nil, err
	}
	return wire.NewInspector(reg, wire.InspectOptions{ResolveDeltas: resolveDeltas})
}

func readInputBytes(a *app, target string) ([]byte, error) {
	if target == "-" {
		if a.stdin == nil {
			return nil, errors.New("stdin is not configured")
		}
		return io.ReadAll(a.stdin)
	}
	return os.ReadFile(target)
}

func runNetDecode(a *app, target, schemaPath, outFmt string, resolveDeltas bool) error {
	ins, err := buildWireInspector(schemaPath, resolveDeltas)
	if err != nil {
		return err
	}
	data, err := readInputBytes(a, target)
	if err != nil {
		return err
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return writeEncodedOutput(a.stdout, outFmt, []wire.DecodedFrame{})
	}

	if trimmed[0] == '{' {
		records, err := netplay.ReadCapture(bytes.NewReader(data))
		if err != nil {
			return err
		}
		frames := make([]wire.DecodedFrame, 0, len(records))
		for i, rec := range records {
			frames = append(frames, ins.InspectCaptureRecord(i, rec.T, string(rec.Dir), uint32(rec.Peer), uint8(rec.Ch), rec.Seq, rec.Payload))
		}
		return writeEncodedOutput(a.stdout, outFmt, frames)
	}

	raw, err := ins.DecodeRawFrameText(string(trimmed))
	if err != nil {
		return err
	}
	frame := ins.InspectRawFrame(0, raw)
	return writeEncodedOutput(a.stdout, outFmt, frame)
}

func runNetStats(a *app, target, schemaPath, outFmt string) error {
	ins, err := buildWireInspector(schemaPath, false)
	if err != nil {
		return err
	}
	data, err := readInputBytes(a, target)
	if err != nil {
		return err
	}
	records, err := netplay.ReadCapture(bytes.NewReader(data))
	if err != nil {
		return err
	}
	frames := make([]wire.DecodedFrame, 0, len(records))
	for i, rec := range records {
		frames = append(frames, ins.InspectCaptureRecord(i, rec.T, string(rec.Dir), uint32(rec.Peer), uint8(rec.Ch), rec.Seq, rec.Payload))
	}
	stats := ins.ComputeStats(frames)
	if outFmt == "yaml" || outFmt == "json" {
		return writeEncodedOutput(a.stdout, outFmt, stats)
	}
	return writeNetStatsText(a.stdout, stats)
}

func writeEncodedOutput(w io.Writer, outFmt string, v any) error {
	switch outFmt {
	case "yaml":
		enc := yaml.NewEncoder(w)
		enc.SetIndent(2)
		if err := enc.Encode(v); err != nil {
			_ = enc.Close()
			return err
		}
		return enc.Close()
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return enc.Encode(v)
	default:
		return fmt.Errorf("unsupported output format %q", outFmt)
	}
}

func writeNetStatsText(w io.Writer, st wire.CaptureStats) error {
	fmt.Fprintf(w, "Packets: %d (send %d / %d B, recv %d / %d B)\n",
		st.TotalPackets, st.SendPackets, st.SendBytes, st.RecvPackets, st.RecvBytes)
	fmt.Fprintf(w, "Bytes: %d B\n", st.TotalBytes)
	fmt.Fprintf(w, "Duration: %s\n", time.Duration(st.DurationNS))
	if st.DeltaCount > 0 {
		fmt.Fprintf(w, "Mean delta size: %.1f B (%d deltas, %d B)\n", st.MeanDeltaBytes, st.DeltaCount, st.DeltaBytes)
	} else {
		fmt.Fprintln(w, "Mean delta size: 0 B (0 deltas)")
	}

	if len(st.ByType) > 0 {
		fmt.Fprintln(w, "\nBy message type:")
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "TYPE\tPACKETS\tBYTES")
		for _, ts := range st.ByType {
			fmt.Fprintf(tw, "%s\t%d\t%d\n", ts.Type, ts.Packets, ts.Bytes)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}

	if len(st.ByPeer) > 0 {
		fmt.Fprintln(w, "\nBy peer:")
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "PEER\tPACKETS\tBYTES\tSEND_PKTS\tSEND_BYTES\tRECV_PKTS\tRECV_BYTES")
		for _, ps := range st.ByPeer {
			fmt.Fprintf(tw, "%d\t%d\t%d\t%d\t%d\t%d\t%d\n",
				ps.Peer, ps.Packets, ps.Bytes, ps.SendPackets, ps.SendBytes, ps.RecvPackets, ps.RecvBytes)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}
	return nil
}
