package epd

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"time"

	"trollfish-lichess/analyze"
	"trollfish-lichess/fen"
)

const (
	OpCodeAnalysisCountDepth   = "acd"
	OpCodeAnalysisCountNodes   = "acn"
	OpCodeAnalysisCountSeconds = "acs"
	OpCodeBestMove             = "bm"
	OpCodeCentipawnEvaluation  = "ce"
	OpCodeDirectMate           = "dm"
)

type File struct {
	Items []*LineItem
}

func (f File) Save(filename string) error {
	b := []byte(f.String())
	if err := ioutil.WriteFile(filename, b, 0644); err != nil {
		return fmt.Errorf("write file '%s': %v", filename, err)
	}
	return nil
}

func (f File) String() string {
	var sb strings.Builder
	for _, line := range f.Items {
		if line.FEN == "" {
			sb.WriteString(line.RawText)
			sb.WriteByte('\n')
			continue
		}

		sb.WriteString(line.FEN)

		if len(line.Ops) == 0 {
			sb.WriteByte('\n')
			continue
		}

		for i := 0; i < len(line.Ops); i++ {
			op := line.Ops[i]

			sb.WriteByte(' ')
			sb.WriteString(op.OpCode)
			sb.WriteByte(' ')
			sb.WriteString(op.Value)
			sb.WriteByte(';')
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

type LineItem struct {
	FEN     string
	Ops     []Operation
	RawText string
}

type AnalysisOptions struct {
	MinDepth   int
	MaxDepth   int
	MinTime    time.Duration
	MaxTime    time.Duration
	DepthDelta int
}

// ACD returns the value for 'acd', the analysis count depth.
func (item *LineItem) ACD() int {
	for _, op := range item.Ops {
		if op.OpCode == OpCodeAnalysisCountDepth {
			return op.atoi()
		}
	}
	return 0
}

func (item *LineItem) SetInt(opCode string, value int) {
	val := strconv.Itoa(value)
	item.SetString(opCode, val)
}

func (item *LineItem) SetString(opCode, value string) {
	for i, op := range item.Ops {
		if op.OpCode == opCode {
			item.Ops[i].Value = value
			return
		}
	}

	item.Ops = append(item.Ops, Operation{OpCode: opCode, Value: value})
}

func (item *LineItem) Remove(opCode string) {
	for i := 0; i < len(item.Ops); i++ {
		if item.Ops[i].OpCode == opCode {
			item.Ops = append(item.Ops[:i], item.Ops[i+1:]...)
			i--
		}
	}
}

func (item *LineItem) parseRawText() {
	// consume FEN (up to 4th space)
	var (
		spaces          int
		charsInFENField int
		rest            string
	)

	for i := 0; i < len(item.RawText); i++ {
		if item.RawText[i] == ' ' || (item.RawText[i] == ';' && spaces == 3) {
			spaces++
			charsInFENField = 0
			if spaces == 4 {
				item.FEN = item.RawText[:i]
				rest = item.RawText[i+1:]
				break
			}
		} else {
			charsInFENField++
		}
	}

	if spaces < 4 {
		if spaces == 3 && charsInFENField > 0 {
			item.FEN = item.RawText
		}
		return
	}

	// TODO: handle quoted strings
	operations := strings.Split(rest, ";")
	if len(operations) == 0 {
		return
	}

	for _, section := range operations {
		section = strings.TrimSpace(section)

		parts := strings.SplitN(section, " ", 2)
		if len(parts) == 0 {
			continue
		}

		opCode := strings.TrimSpace(parts[0])
		op := Operation{OpCode: opCode}

		if len(parts) == 1 {
			if opCode != "" {
				item.Ops = append(item.Ops, op)
			}
			continue
		}

		op.Value = strings.TrimSpace(parts[1])
		item.Ops = append(item.Ops, op)
	}
}

type Operation struct {
	OpCode string
	Value  string
}

func (op Operation) atoi() int {
	n, err := strconv.Atoi(op.Value)
	if err != nil {
		return 0
	}
	return n
}

func ReadFile(filename string) (File, error) {
	b, err := ioutil.ReadFile(filename)
	if err != nil {
		return File{}, fmt.Errorf("file '%s': %v", filename, err)
	}

	return ParseText(string(b)), nil
}

func ParseText(text string) File {
	var file File

	lines := strings.Split(text, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		// skip the last empty line
		if len(line) == 0 && i == len(lines)-1 {
			break
		}
		item := LineItem{RawText: line}
		item.parseRawText()

		file.Items = append(file.Items, &item)
	}

	return file
}

func UpdateFile(ctx context.Context, filename string, opts AnalysisOptions) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	file, err := ReadFile(filename)
	if err != nil {
		return err
	}

	tempFilename := filename + ".new"
	bakFilename := filename + ".bak"

	for _, fn := range []string{tempFilename, bakFilename} {
		if fileExists(fn) {
			return fmt.Errorf("file '%s' already exists, please remove it before updating this EPD file", fn)
		}
	}

	filtered := func() []*LineItem {
		var items []*LineItem
		for _, item := range file.Items {
			if item.FEN == "" || item.ACD() >= opts.MinDepth {
				continue
			}
			items = append(items, item)
		}
		return items
	}()

	a := analyze.New()

	wg, err := a.StartStockfish(ctx)
	if err != nil {
		return err
	}

	for i := 0; i < len(filtered); i++ {
		item := filtered[i]
		evals, err := a.AnalyzePosition(ctx, item.FEN)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		if len(evals) == 0 {
			continue
		}

		bestMove := evals[0]

		uci := bestMove.UCIMove
		b := fen.FENtoBoard(item.FEN)
		san := b.UCItoSAN(uci)

		item.SetString(OpCodeBestMove, san)
		item.SetString("bm_uci", uci)
		item.SetInt(OpCodeAnalysisCountDepth, bestMove.Depth)
		item.SetInt(OpCodeAnalysisCountNodes, bestMove.Nodes)
		item.SetInt(OpCodeAnalysisCountSeconds, bestMove.Nodes)

		if bestMove.Mate == 0 {
			item.SetInt(OpCodeCentipawnEvaluation, bestMove.POVCP(b.ActiveColor))
			item.Remove(OpCodeDirectMate)
		} else {
			item.SetInt(OpCodeDirectMate, bestMove.POVMate(b.ActiveColor))
			item.Remove(OpCodeCentipawnEvaluation)
		}

		if err := ioutil.WriteFile(tempFilename, []byte(file.String()), 0644); err != nil {
			return fmt.Errorf("error writing file '%s': %v", tempFilename, err)
		}
	}

	if err := os.Rename(filename, bakFilename); err != nil {
		return fmt.Errorf("error renaming '%s' to '%s': %v", filename, bakFilename, err)
	}
	if err := os.Rename(tempFilename, filename); err != nil {
		return fmt.Errorf("error renaming '%s' to '%s': %v", tempFilename, filename, err)
	}

	cancel()

	if wg != nil {
		wg.Wait()
	}

	return nil
}

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return true
}