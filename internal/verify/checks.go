package verify

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, used read-only on restored copies

	"backupharbor/internal/config"
	"backupharbor/internal/i18n"
)

// fileCheck is the outcome of checking one restored file.
type fileCheck struct {
	status Status
	msg    i18n.Msg
}

func ok(code string, args ...any) fileCheck {
	return fileCheck{Passed, i18n.M(code, args...)}
}
func bad(code string, args ...any) fileCheck {
	return fileCheck{Failed, i18n.M(code, args...)}
}
func partial(code string, args ...any) fileCheck {
	return fileCheck{Partial, i18n.M(code, args...)}
}

// maxZIPUncompressed bounds how much data is decompressed while checking one
// archive (protection against decompression bombs).
var maxZIPUncompressed int64 = 2 << 30

// ctxReader aborts reads once the context is done so long checks can be cancelled.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// checkFile runs the rule's check against one restored file.
func checkFile(ctx context.Context, rule config.Rule, abs string, size int64) fileCheck {
	var fc fileCheck
	switch rule.Type {
	case config.RuleExists:
		fc = ok("check.exists")
	case config.RuleNonEmpty:
		if size > 0 {
			fc = ok("check.non_empty", size)
		} else {
			fc = bad("check.empty")
		}
	case config.RuleText:
		fc = checkText(ctx, abs)
	case config.RuleJSON:
		fc = checkJSON(ctx, abs)
	case config.RuleXML:
		fc = checkXML(ctx, abs)
	case config.RuleZIP:
		fc = checkZIP(ctx, abs)
	case config.RulePDF:
		fc = checkPDF(abs, size)
	case config.RuleSQLite:
		fc = checkSQLite(ctx, abs)
	case config.RuleSHA256:
		fc = checkSHA256(ctx, abs, rule.SHA256)
	default:
		fc = bad("check.unknown_type", rule.Type)
	}
	// A cancelled context must never be reported as a content failure.
	if ctx.Err() != nil {
		return cancelledCheck()
	}
	return fc
}

func openFile(abs string) (*os.File, error) { return os.Open(abs) }

func readErr(err error) fileCheck { return bad("check.read_error", err.Error()) }

func checkText(ctx context.Context, abs string) fileCheck {
	f, err := openFile(abs)
	if err != nil {
		return readErr(err)
	}
	defer f.Close()
	r := ctxReader{ctx, f}
	buf := make([]byte, 64<<10)
	var carry []byte
	first := true
	utf16 := false
	var total int64
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if first {
				first = false
				if bytes.HasPrefix(chunk, []byte{0xFF, 0xFE}) || bytes.HasPrefix(chunk, []byte{0xFE, 0xFF}) {
					utf16 = true
				}
			}
			total += int64(n)
			if !utf16 {
				if bytes.IndexByte(chunk, 0) >= 0 {
					return bad("check.text_binary")
				}
				data := append(carry, chunk...)
				// Keep an incomplete trailing rune for the next chunk.
				cut := len(data)
				for i := 1; i <= 3 && i <= len(data); i++ {
					if !utf8.FullRune(data[len(data)-i:]) {
						cut = len(data) - i
						break
					}
				}
				if !utf8.Valid(data[:cut]) {
					return bad("check.text_invalid_utf8")
				}
				carry = append([]byte{}, data[cut:]...)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return readErr(err)
		}
	}
	if utf16 && total%2 != 0 {
		return bad("check.text_utf16_odd")
	}
	if len(carry) > 0 {
		return bad("check.text_invalid_utf8")
	}
	return ok("check.text", total)
}

func checkJSON(ctx context.Context, abs string) fileCheck {
	f, err := openFile(abs)
	if err != nil {
		return readErr(err)
	}
	defer f.Close()
	dec := json.NewDecoder(ctxReader{ctx, f})
	depth := 0
	started := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			if !started {
				return bad("check.json_empty")
			}
			if depth != 0 {
				return bad("check.json_invalid", "unexpected end of JSON input")
			}
			return ok("check.json")
		}
		if err != nil {
			if ctx.Err() != nil {
				return cancelledCheck()
			}
			return bad("check.json_invalid", err.Error())
		}
		if depth == 0 && started {
			return bad("check.json_trailing")
		}
		started = true
		if d, isDelim := tok.(json.Delim); isDelim {
			if d == '{' || d == '[' {
				depth++
			} else {
				depth--
			}
		}
	}
}

func checkXML(ctx context.Context, abs string) fileCheck {
	f, err := openFile(abs)
	if err != nil {
		return readErr(err)
	}
	defer f.Close()
	dec := xml.NewDecoder(ctxReader{ctx, f})
	dec.Strict = true
	elements := 0
	depth := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			if elements == 0 {
				return bad("check.xml_empty")
			}
			if depth != 0 {
				return bad("check.xml_invalid", "unexpected end of document")
			}
			return ok("check.xml", elements)
		}
		if err != nil {
			if ctx.Err() != nil {
				return cancelledCheck()
			}
			return bad("check.xml_invalid", err.Error())
		}
		switch tok.(type) {
		case xml.StartElement:
			elements++
			depth++
		case xml.EndElement:
			depth--
		}
	}
}

func checkZIP(ctx context.Context, abs string) fileCheck {
	zr, err := zip.OpenReader(abs)
	if err != nil {
		return bad("check.zip_invalid", err.Error())
	}
	defer zr.Close()
	var total int64
	capped := false
	for _, zf := range zr.File {
		if err := ctx.Err(); err != nil {
			return cancelledCheck()
		}
		if zf.FileInfo().IsDir() {
			continue
		}
		if total >= maxZIPUncompressed {
			capped = true
			break
		}
		rc, err := zf.Open()
		if err != nil {
			return bad("check.zip_entry", zf.Name, err.Error())
		}
		// Reading to EOF makes archive/zip verify the entry's CRC-32.
		n, err := io.Copy(io.Discard, io.LimitReader(ctxReader{ctx, rc}, maxZIPUncompressed-total+1))
		rc.Close()
		total += n
		if err != nil {
			if ctx.Err() != nil {
				return cancelledCheck()
			}
			return bad("check.zip_entry", zf.Name, err.Error())
		}
		if total > maxZIPUncompressed {
			capped = true
			break
		}
	}
	if capped {
		return partial("check.zip_capped", len(zr.File))
	}
	return ok("check.zip", len(zr.File), total)
}

var reStartXref = regexp.MustCompile(`startxref\s+(\d+)`)

// checkPDF performs structural checks only: header, trailer marker and that the
// startxref offset points at a cross-reference table or stream. It does not
// render or fully parse the document.
func checkPDF(abs string, size int64) fileCheck {
	f, err := openFile(abs)
	if err != nil {
		return readErr(err)
	}
	defer f.Close()
	if size < 20 {
		return bad("check.pdf_too_small")
	}
	head := make([]byte, 1024)
	n, _ := f.ReadAt(head, 0)
	if !bytes.Contains(head[:n], []byte("%PDF-")) {
		return bad("check.pdf_header")
	}
	tailLen := int64(2048)
	if size < tailLen {
		tailLen = size
	}
	tail := make([]byte, tailLen)
	n, _ = f.ReadAt(tail, size-tailLen)
	tail = tail[:n]
	if !bytes.Contains(tail, []byte("%%EOF")) {
		return bad("check.pdf_eof")
	}
	m := reStartXref.FindSubmatch(tail)
	if m == nil {
		return bad("check.pdf_startxref")
	}
	off, err := strconv.ParseInt(string(m[1]), 10, 64)
	if err != nil || off < 0 || off >= size {
		return bad("check.pdf_xref_offset")
	}
	probe := make([]byte, 64)
	n, _ = f.ReadAt(probe, off)
	p := bytes.TrimLeft(probe[:n], " \r\n\t")
	if !(bytes.HasPrefix(p, []byte("xref")) || regexp.MustCompile(`^\d+\s+\d+\s+obj`).Match(p)) {
		return bad("check.pdf_xref_target")
	}
	return ok("check.pdf")
}

var sqliteMagic = []byte("SQLite format 3\x00")

func checkSQLite(ctx context.Context, abs string) fileCheck {
	f, err := openFile(abs)
	if err != nil {
		return readErr(err)
	}
	hdr := make([]byte, 16)
	n, _ := io.ReadFull(f, hdr)
	f.Close()
	if n < 16 || !bytes.Equal(hdr, sqliteMagic) {
		return bad("check.sqlite_header")
	}
	p := filepath.ToSlash(abs)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // file:///C:/dir/file.db
	}
	u := url.URL{Scheme: "file", Path: p, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return bad("check.sqlite_open", err.Error())
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	rows, err := db.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		if ctx.Err() != nil {
			return cancelledCheck()
		}
		return bad("check.sqlite_open", err.Error())
	}
	defer rows.Close()
	var problems []string
	count := 0
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return bad("check.sqlite_open", err.Error())
		}
		count++
		if line != "ok" && len(problems) < 5 {
			problems = append(problems, line)
		}
	}
	if err := rows.Err(); err != nil {
		if ctx.Err() != nil {
			return cancelledCheck()
		}
		return bad("check.sqlite_open", err.Error())
	}
	if len(problems) > 0 || count == 0 {
		return bad("check.sqlite_integrity", strings.Join(problems, "; "))
	}
	return ok("check.sqlite")
}

func checkSHA256(ctx context.Context, abs, want string) fileCheck {
	f, err := openFile(abs)
	if err != nil {
		return readErr(err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, ctxReader{ctx, f}); err != nil {
		if ctx.Err() != nil {
			return cancelledCheck()
		}
		return readErr(err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, want) {
		return bad("check.sha256_mismatch", got)
	}
	return ok("check.sha256")
}

func cancelledCheck() fileCheck { return fileCheck{Cancelled, i18n.M("check.interrupted")} }
