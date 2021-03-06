package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"sort"
	"strings"

	"github.com/govim/govim/cmd/govim/internal/golang_org_x_tools/lsp/protocol"
	"github.com/govim/govim/cmd/govim/internal/golang_org_x_tools/span"
	"github.com/govim/govim/cmd/govim/internal/types"
)

const (
	exprAutocmdCurrBufInfo = `{"Num": eval(expand('<abuf>')), "Name": fnamemodify(bufname(eval(expand('<abuf>'))),':p'), "Contents": join(getbufline(eval(expand('<abuf>')), 0, "$"), "\n")."\n", "Loaded": bufloaded(eval(expand('<abuf>')))}`
)

// currentBufferInfo is a helper function to unmarshal autocmd current
// buffer details from expr
func (v *vimstate) currentBufferInfo(expr json.RawMessage) *types.Buffer {
	var buf struct {
		Num      int
		Name     string
		Contents string
		Loaded   int
	}
	v.Parse(expr, &buf)
	return types.NewBuffer(buf.Num, buf.Name, []byte(buf.Contents), buf.Loaded == 1)
}

type cursorPosition struct {
	BufNr int `json:"bufnr"`
	Line  int `json:"line"`
	Col   int `json:"col"`
}

const cursorPositionExpr = `{"bufnr": bufnr(""), "line": line("."), "col": col(".")}`

func (v *vimstate) cursorPos() (b *types.Buffer, p types.Point, err error) {
	var pos cursorPosition
	expr := v.ChannelExpr(cursorPositionExpr)
	v.Parse(expr, &pos)
	b, ok := v.buffers[pos.BufNr]
	if !ok {
		err = fmt.Errorf("failed to resolve buffer %v", pos.BufNr)
		return
	}
	p, err = types.PointFromVim(b, pos.Line, pos.Col)
	return
}

// populateQuickfix populates and opens a quickfix window with a sorted
// slice of locations. If shift is true the first element of the slice
// will be skipped.
func (v *vimstate) populateQuickfix(locs []protocol.Location, shift bool) {
	// must be non-nil
	qf := []quickfixEntry{}

	for _, loc := range locs {
		var buf *types.Buffer
		for _, b := range v.buffers {
			if b.Loaded && b.URI() == span.URI(loc.URI) {
				buf = b
			}
		}
		fn := span.URI(loc.URI).Filename()
		if buf == nil {
			byts, err := ioutil.ReadFile(fn)
			if err != nil {
				v.Logf("populateQuickfix: failed to read contents of %v: %v", fn, err)
				continue
			}
			// create a temp buffer
			buf = types.NewBuffer(-1, fn, byts, false)
		}
		// make fn relative for reporting purposes
		fn, err := filepath.Rel(v.workingDirectory, fn)
		if err != nil {
			v.Logf("populateQuickfix: failed to call filepath.Rel(%q, %q): %v", v.workingDirectory, fn, err)
			continue
		}
		p, err := types.PointFromPosition(buf, loc.Range.Start)
		if err != nil {
			v.Logf("popularQuickfix: failed to resolve position: %v", err)
			continue
		}
		line, err := buf.Line(p.Line())
		if err != nil {
			v.Logf("popularQuickfix: location invalid in buffer: %v", err)
			continue
		}
		qf = append(qf, quickfixEntry{
			Filename: fn,
			Lnum:     p.Line(),
			Col:      p.Col(),
			Text:     line,
		})
	}

	var toSort []quickfixEntry

	if shift {
		toSort = qf[1:]
	} else {
		toSort = qf
	}

	sort.Slice(toSort, func(i, j int) bool {
		lhs, rhs := toSort[i], toSort[j]
		cmp := strings.Compare(lhs.Filename, rhs.Filename)
		if cmp != 0 {
			if lhs.Filename == qf[0].Filename {
				return true
			} else if rhs.Filename == qf[0].Filename {
				return false
			}
		}
		if cmp == 0 {
			cmp = lhs.Lnum - rhs.Lnum
		}
		if cmp == 0 {
			cmp = lhs.Col - rhs.Col
		}
		return cmp < 0
	})

	v.ChannelCall("setqflist", qf, "r")
	v.ChannelEx("copen")
}
