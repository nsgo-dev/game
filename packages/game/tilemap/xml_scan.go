package tilemap

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/nsgo-dev/game/packages/game/loaderr"
)

const maxXMLStackDepth = 512

type xmlName struct {
	Local string
}

type xmlAttr struct {
	Name  xmlName
	Value string
}

type xmlStartElement struct {
	Name xmlName
	Attr []xmlAttr
}

type xmlEndElement struct {
	Name xmlName
}

type xmlCharData []byte

// xmlReader is a compact streaming XML 1.0 tokenizer tailored for Tiled TMX and TSX files.
// Avoiding encoding/xml keeps the WebAssembly binary within its size budget while preserving
// strict tag-nesting checks, entity decoding, and 1-based line/column diagnostics.
type xmlReader struct {
	data        []byte
	off         int
	pos         xmlPos
	fix         string
	stack       []string
	hasPending  bool
	pendingName string
	pendingLine int
	pendingCol  int
}

func newXMLReader(data []byte, fix string) *xmlReader {
	clean := bytes.TrimPrefix(data, utf8BOM)
	return &xmlReader{
		data: clean,
		pos:  newXMLPos(clean),
		fix:  fix,
	}
}

func (r *xmlReader) syntaxErrAt(off int, msg string, truncated bool) error {
	line, col := r.pos.at(off)
	fix := loaderr.FixSyntax
	if truncated {
		fix = loaderr.FixTruncated
	}
	return &loaderr.Error{
		Line:    line,
		Column:  col,
		Problem: "invalid XML: " + msg,
		Fix:     fix,
	}
}

func (r *xmlReader) syntaxErrMsg(off int, truncated bool, prefix, name, suffix string) error {
	return r.syntaxErrAt(off, prefix+name+suffix, truncated)
}

func (r *xmlReader) skip() error {
	depth := 1
	for depth > 0 {
		tok, _, _, err := r.nextToken()
		if err == io.EOF {
			return r.syntaxErrAt(r.off, "unexpected EOF while skipping element", true)
		}
		if err != nil {
			return err
		}
		switch tok.(type) {
		case xmlStartElement:
			depth++
		case xmlEndElement:
			depth--
		}
	}
	return nil
}

// nextToken returns the next XML token together with the 1-based (line, column) where it starts.
func (r *xmlReader) nextToken() (any, int, int, error) {
	if r.hasPending {
		r.hasPending = false
		return xmlEndElement{Name: xmlName{Local: xmlLocalName(r.pendingName)}}, r.pendingLine, r.pendingCol, nil
	}

	for {
		if r.off >= len(r.data) {
			return nil, 0, 0, io.EOF
		}

		start := r.off
		if r.data[r.off] != '<' {
			// Character data up to the next '<'.
			lt := bytes.IndexByte(r.data[r.off:], '<')
			var raw []byte
			if lt < 0 {
				raw = r.data[r.off:]
				r.off = len(r.data)
			} else {
				raw = r.data[r.off : r.off+lt]
				r.off += lt
			}
			text, err := unescapeXML(raw, start, r)
			if err != nil {
				return nil, 0, 0, err
			}
			line, col := r.pos.at(start)
			return xmlCharData(text), line, col, nil
		}

		// Starts with '<'.
		if r.off+1 >= len(r.data) {
			return nil, 0, 0, r.syntaxErrAt(start, "unexpected EOF after '<'", true)
		}
		next := r.data[r.off+1]
		switch next {
		case '?':
			// Processing instruction <? ... ?>
			end := bytes.Index(r.data[r.off+2:], []byte("?>"))
			if end < 0 {
				return nil, 0, 0, r.syntaxErrAt(start, "unterminated processing instruction", true)
			}
			r.off += 2 + end + 2
			continue

		case '!':
			if bytes.HasPrefix(r.data[r.off:], []byte("<!--")) {
				end := bytes.Index(r.data[r.off+4:], []byte("-->"))
				if end < 0 {
					return nil, 0, 0, r.syntaxErrAt(start, "unterminated comment", true)
				}
				r.off += 4 + end + 3
				continue
			}
			if bytes.HasPrefix(r.data[r.off:], []byte("<![CDATA[")) {
				end := bytes.Index(r.data[r.off+9:], []byte("]]>"))
				if end < 0 {
					return nil, 0, 0, r.syntaxErrAt(start, "unterminated CDATA section", true)
				}
				body := normalizeXMLNewlines(r.data[r.off+9 : r.off+9+end])
				r.off += 9 + end + 3
				line, col := r.pos.at(start)
				return xmlCharData(body), line, col, nil
			}
			// DOCTYPE or other declaration <! ... >
			if err := r.skipDirective(start); err != nil {
				return nil, 0, 0, err
			}
			continue

		case '/':
			// Closing tag </name>
			r.off += 2
			name, err := r.scanName()
			if err != nil {
				return nil, 0, 0, err
			}
			r.skipWS()
			if r.off >= len(r.data) {
				return nil, 0, 0, r.syntaxErrAt(start, "unexpected EOF in closing tag", true)
			}
			if r.data[r.off] != '>' {
				return nil, 0, 0, r.syntaxErrMsg(r.off, false, "expected '>' to close </", name, ">")
			}
			r.off++
			if len(r.stack) == 0 {
				return nil, 0, 0, r.syntaxErrMsg(start, false, "unexpected closing tag </", name, ">")
			}
			open := r.stack[len(r.stack)-1]
			if open != name {
				return nil, 0, 0, r.syntaxErrMsg(start, false, "element <"+open+"> closed by </", name, ">")
			}
			r.stack = r.stack[:len(r.stack)-1]
			line, col := r.pos.at(start)
			return xmlEndElement{Name: xmlName{Local: xmlLocalName(name)}}, line, col, nil

		default:
			// Start tag <name attr="val" ...> or <name .../>
			r.off++
			name, err := r.scanName()
			if err != nil {
				return nil, 0, 0, err
			}
			var attrs []xmlAttr
			selfClosing := false
			for {
				wsBefore := r.skipWS()
				if r.off >= len(r.data) {
					return nil, 0, 0, r.syntaxErrMsg(start, true, "unexpected EOF in <", name, ">")
				}
				b := r.data[r.off]
				if b == '>' {
					r.off++
					break
				}
				if b == '/' {
					if r.off+1 >= len(r.data) {
						return nil, 0, 0, r.syntaxErrMsg(start, true, "unexpected EOF in <", name, "/>")
					}
					if r.data[r.off+1] != '>' {
						return nil, 0, 0, r.syntaxErrAt(r.off, "expected '>' after '/'", false)
					}
					r.off += 2
					selfClosing = true
					break
				}
				if !wsBefore {
					return nil, 0, 0, r.syntaxErrMsg(r.off, false, "expected whitespace before attribute in <", name, ">")
				}
				attrName, err := r.scanName()
				if err != nil {
					return nil, 0, 0, err
				}
				r.skipWS()
				if r.off >= len(r.data) || r.data[r.off] != '=' {
					return nil, 0, 0, r.syntaxErrMsg(r.off, r.off >= len(r.data), "expected '=' after attribute ", attrName, "")
				}
				r.off++
				r.skipWS()
				if r.off >= len(r.data) {
					return nil, 0, 0, r.syntaxErrMsg(r.off, true, "unexpected EOF reading attribute ", attrName, "")
				}
				quote := r.data[r.off]
				if quote != '"' && quote != '\'' {
					return nil, 0, 0, r.syntaxErrMsg(r.off, false, "attribute ", attrName, " value must be quoted")
				}
				r.off++
				valStart := r.off
				closeIdx := bytes.IndexByte(r.data[valStart:], quote)
				if closeIdx < 0 {
					return nil, 0, 0, r.syntaxErrMsg(valStart, true, "unterminated attribute ", attrName, "")
				}
				rawVal := r.data[valStart : valStart+closeIdx]
				if bytes.IndexByte(rawVal, '<') >= 0 {
					return nil, 0, 0, r.syntaxErrMsg(valStart, false, "unescaped '<' in attribute ", attrName, "")
				}
				val, err := unescapeXML(rawVal, valStart, r)
				if err != nil {
					return nil, 0, 0, err
				}
				r.off = valStart + closeIdx + 1
				attrs = append(attrs, xmlAttr{
					Name:  xmlName{Local: xmlLocalName(attrName)},
					Value: string(val),
				})
			}

			line, col := r.pos.at(start)
			if selfClosing {
				r.hasPending = true
				r.pendingName = name
				r.pendingLine = line
				r.pendingCol = col
			} else {
				if len(r.stack) >= maxXMLStackDepth {
					return nil, 0, 0, r.syntaxErrAt(start, "XML nesting depth exceeds 512", false)
				}
				r.stack = append(r.stack, name)
			}
			return xmlStartElement{
				Name: xmlName{Local: xmlLocalName(name)},
				Attr: attrs,
			}, line, col, nil
		}
	}
}

func (r *xmlReader) skipDirective(start int) error {
	r.off += 2 // skip "<!"
	bracketDepth := 0
	var quote byte
	for r.off < len(r.data) {
		b := r.data[r.off]
		r.off++
		if quote != 0 {
			if b == quote {
				quote = 0
			}
			continue
		}
		switch b {
		case '"', '\'':
			quote = b
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case '>':
			if bracketDepth == 0 {
				return nil
			}
		}
	}
	return r.syntaxErrAt(start, "unterminated XML directive", true)
}

func (r *xmlReader) skipWS() bool {
	start := r.off
	for r.off < len(r.data) {
		switch r.data[r.off] {
		case ' ', '\t', '\r', '\n':
			r.off++
		default:
			return r.off > start
		}
	}
	return r.off > start
}

func (r *xmlReader) scanName() (string, error) {
	start := r.off
	for r.off < len(r.data) {
		b := r.data[r.off]
		if b < 0x80 {
			if !isXMLNameByte(b, r.off == start) {
				break
			}
			r.off++
			continue
		}
		_, size := utf8.DecodeRune(r.data[r.off:])
		r.off += size
	}
	if r.off == start {
		return "", r.syntaxErrAt(start, "expected XML name", r.off >= len(r.data))
	}
	return string(r.data[start:r.off]), nil
}

func isXMLNameByte(b byte, first bool) bool {
	if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_' || b == ':' {
		return true
	}
	if !first && ((b >= '0' && b <= '9') || b == '-' || b == '.') {
		return true
	}
	return false
}

func xmlLocalName(qname string) string {
	if _, local, ok := strings.Cut(qname, ":"); ok {
		return local
	}
	return qname
}

func normalizeXMLNewlines(raw []byte) []byte {
	if bytes.IndexByte(raw, '\r') < 0 {
		return raw
	}
	out := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\r' {
			out = append(out, '\n')
			if i+1 < len(raw) && raw[i+1] == '\n' {
				i++
			}
			continue
		}
		out = append(out, raw[i])
	}
	return out
}

func unescapeXML(raw []byte, baseOff int, r *xmlReader) ([]byte, error) {
	if bytes.IndexByte(raw, '&') < 0 && bytes.IndexByte(raw, '\r') < 0 {
		return raw, nil
	}
	out := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		b := raw[i]
		if b == '\r' {
			out = append(out, '\n')
			if i+1 < len(raw) && raw[i+1] == '\n' {
				i++
			}
			continue
		}
		if b != '&' {
			out = append(out, b)
			continue
		}
		semi := bytes.IndexByte(raw[i+1:], ';')
		if semi < 0 {
			return nil, r.syntaxErrAt(baseOff+i, "unterminated XML entity reference", false)
		}
		ent := string(raw[i+1 : i+1+semi])
		switch ent {
		case "amp":
			out = append(out, '&')
		case "lt":
			out = append(out, '<')
		case "gt":
			out = append(out, '>')
		case "quot":
			out = append(out, '"')
		case "apos":
			out = append(out, '\'')
		default:
			if strings.HasPrefix(ent, "#") {
				var code uint64
				var err error
				if strings.HasPrefix(ent, "#x") || strings.HasPrefix(ent, "#X") {
					code, err = strconv.ParseUint(ent[2:], 16, 32)
				} else {
					code, err = strconv.ParseUint(ent[1:], 10, 32)
				}
				if err != nil || code == 0 || code > utf8.MaxRune || !utf8.ValidRune(rune(code)) {
					return nil, r.syntaxErrMsg(baseOff+i, false, "invalid numeric character reference &", ent, ";")
				}
				out = utf8.AppendRune(out, rune(code))
			} else {
				return nil, r.syntaxErrMsg(baseOff+i, false, "unknown XML entity &", ent, ";")
			}
		}
		i += 1 + semi
	}
	return out, nil
}

var (
	errInvalidZlib = errors.New("invalid zlib header or checksum")
	errInvalidGzip = errors.New("invalid gzip header or checksum")
)

// decompressZlib inflates an RFC 1950 zlib stream using compress/flate directly,
// verifying the 2-byte header and 4-byte Adler-32 trailer while capping output at limit bytes.
func decompressZlib(raw []byte, limit int64) ([]byte, bool, error) {
	if len(raw) < 6 {
		return nil, false, errInvalidZlib
	}
	cmf, flg := raw[0], raw[1]
	if cmf&0x0f != 8 || cmf>>4 > 7 || (uint16(cmf)*256+uint16(flg))%31 != 0 || flg&0x20 != 0 {
		return nil, false, errInvalidZlib
	}
	br := bytes.NewReader(raw[2:])
	fr := flate.NewReader(br)
	out, err := io.ReadAll(io.LimitReader(fr, limit+1))
	_ = fr.Close()
	if err != nil {
		return nil, false, err
	}
	if int64(len(out)) > limit {
		return nil, true, nil
	}
	var trailer [4]byte
	if _, err := io.ReadFull(br, trailer[:]); err != nil {
		return nil, false, errInvalidZlib
	}
	if binary.BigEndian.Uint32(trailer[:]) != adler32Sum(out) {
		return nil, false, errInvalidZlib
	}
	return out, false, nil
}

// decompressGzip inflates an RFC 1952 gzip stream (one or more members) using compress/flate directly,
// verifying each member's header, IEEE CRC-32, and ISIZE while capping total output at limit bytes.
func decompressGzip(raw []byte, limit int64) ([]byte, bool, error) {
	br := bytes.NewReader(raw)
	var total []byte
	for {
		var hdr [10]byte
		if _, err := io.ReadFull(br, hdr[:]); err != nil {
			return nil, false, errInvalidGzip
		}
		if hdr[0] != 0x1f || hdr[1] != 0x8b || hdr[2] != 8 {
			return nil, false, errInvalidGzip
		}
		flg := hdr[3]
		if flg&0x04 != 0 { // FEXTRA
			var xlen [2]byte
			if _, err := io.ReadFull(br, xlen[:]); err != nil {
				return nil, false, errInvalidGzip
			}
			if _, err := io.CopyN(io.Discard, br, int64(binary.LittleEndian.Uint16(xlen[:]))); err != nil {
				return nil, false, errInvalidGzip
			}
		}
		if flg&0x08 != 0 { // FNAME
			if err := skipZeroTerminated(br); err != nil {
				return nil, false, errInvalidGzip
			}
		}
		if flg&0x10 != 0 { // FCOMMENT
			if err := skipZeroTerminated(br); err != nil {
				return nil, false, errInvalidGzip
			}
		}
		if flg&0x02 != 0 { // FHCRC
			var hcrc [2]byte
			if _, err := io.ReadFull(br, hcrc[:]); err != nil {
				return nil, false, errInvalidGzip
			}
		}

		rem := limit - int64(len(total))
		fr := flate.NewReader(br)
		part, err := io.ReadAll(io.LimitReader(fr, rem+1))
		_ = fr.Close()
		if err != nil {
			return nil, false, err
		}
		if int64(len(part)) > rem {
			return nil, true, nil
		}
		var trailer [8]byte
		if _, err := io.ReadFull(br, trailer[:]); err != nil {
			return nil, false, errInvalidGzip
		}
		wantCRC := binary.LittleEndian.Uint32(trailer[0:4])
		wantSize := binary.LittleEndian.Uint32(trailer[4:8])
		if wantCRC != crc32IEEE(part) || wantSize != uint32(len(part)) {
			return nil, false, errInvalidGzip
		}
		if len(total) == 0 {
			total = part
		} else {
			total = append(total, part...)
		}
		if br.Len() == 0 {
			return total, false, nil
		}
	}
}

func skipZeroTerminated(br *bytes.Reader) error {
	for {
		b, err := br.ReadByte()
		if err != nil {
			return err
		}
		if b == 0 {
			return nil
		}
	}
}

func adler32Sum(p []byte) uint32 {
	const mod = 65521
	var s1, s2 uint32 = 1, 0
	for _, b := range p {
		s1 += uint32(b)
		if s1 >= mod {
			s1 -= mod
		}
		s2 += s1
		if s2 >= mod {
			s2 -= mod
		}
	}
	return s2<<16 | s1
}

func crc32IEEE(p []byte) uint32 {
	crc := ^uint32(0)
	for _, b := range p {
		crc ^= uint32(b)
		for range 8 {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xedb88320
			} else {
				crc >>= 1
			}
		}
	}
	return ^crc
}

// walkChildren reads tokens inside an open element until its matching end tag,
// calling onText (if non-nil) for character data and onChild for start tags.
// If onChild returns (false, nil), the child element is skipped automatically.
func (r *xmlReader) walkChildren(
	line, col int,
	field, tag string,
	onText func(xmlCharData),
	onChild func(xmlStartElement, int, int) (bool, error),
) error {
	for {
		tok, cl, cc, err := r.nextToken()
		if err == io.EOF {
			return xmlEOFErr(line, col, field, tag)
		}
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xmlEndElement:
			return nil
		case xmlCharData:
			if onText != nil {
				onText(t)
			}
		case xmlStartElement:
			if onChild != nil {
				handled, err := onChild(t, cl, cc)
				if err != nil {
					return err
				}
				if handled {
					continue
				}
			}
			if err := r.skip(); err != nil {
				return err
			}
		}
	}
}
