package epd

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
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
	OpCodeSuppliedMove         = "sm"
)

type File struct {
	Lines []*LineItem
}

func (f *File) Contains(fenKey string) bool {
	fenKey = fenToKey(fenKey)
	if len(fenKey) == 0 {
		return false
	}

	for _, item := range f.Lines {
		if item.FEN == fenKey {
			return true
		}
	}

	return false
}

func (f *File) Add(fenKey string, ops ...Operation) *LineItem {
	fenKey = fenToKey(fenKey)
	line := &LineItem{FEN: fenKey}
	for _, op := range ops {
		line.Ops = append(line.Ops, op)
	}
	f.Lines = append(f.Lines, line)
	line.RawText = line.String()
	return line
}

func (f *File) Find(fenKey string) string {
	fenKey = fenToKey(fenKey)
	if len(fenKey) == 0 {
		return ""
	}

	for _, item := range f.Lines {
		if item.FEN == fenKey {
			return item.String()
		}
	}

	return ""
}

func (f *File) Save(filename string, backup bool) error {
	b := []byte(f.String())
	if backup && fileExists(filename) {
		ext := filepath.Ext(filename)
		backupFilename := fmt.Sprintf("%s-%d%s.backup", strings.TrimSuffix(filename, ext), time.Now().UnixMilli(), ext)
		if err := os.Rename(filename, backupFilename); err != nil {
			return fmt.Errorf("error creating backup file '%s': %v", backupFilename, err)
		}
	}
	if err := ioutil.WriteFile(filename, b, 0644); err != nil {
		return fmt.Errorf("write file '%s': %v", filename, err)
	}
	return nil
}

func (f *File) String() string {
	var sb strings.Builder
	for _, line := range f.Lines {
		sb.WriteString(line.String())
		sb.WriteByte('\n')
	}
	return sb.String()
}

type LineItem struct {
	FEN     string
	Ops     []Operation
	RawText string
}

func (line *LineItem) String() string {
	if line.FEN == "" {
		return line.RawText
	}

	var sb strings.Builder
	sb.WriteString(line.FEN)
	for _, op := range line.Ops {
		sb.WriteByte(' ')
		sb.WriteString(op.OpCode)
		sb.WriteByte(' ')
		sb.WriteString(op.Value)
		sb.WriteByte(';')
	}

	return sb.String()
}

type AnalysisOptions struct {
	MinDepth   int
	MaxDepth   int
	MinTime    time.Duration
	MaxTime    time.Duration
	DepthDelta int
}

// ACD returns the value for 'acd', the analysis count depth.
func (line *LineItem) ACD() int {
	return line.GetInt(OpCodeAnalysisCountDepth)
}

func (line *LineItem) BestMove() string {
	return line.GetString(OpCodeBestMove)
}

func (line *LineItem) GetInt(opCode string) int {
	for _, op := range line.Ops {
		if op.OpCode == opCode {
			return op.atoi()
		}
	}
	return 0
}

func (line *LineItem) GetString(opCode string) string {
	for _, op := range line.Ops {
		if op.OpCode == opCode {
			return op.Value
		}
	}
	return ""
}

func (line *LineItem) SetInt(opCode string, value int) {
	val := strconv.Itoa(value)
	line.SetString(opCode, val)
}

func (line *LineItem) SetString(opCode, value string) {
	for i, op := range line.Ops {
		if op.OpCode == opCode {
			line.Ops[i].Value = value
			return
		}
	}

	line.Ops = append(line.Ops, Operation{OpCode: opCode, Value: value})
}

func (line *LineItem) Remove(opCode string) {
	for i := 0; i < len(line.Ops); i++ {
		if line.Ops[i].OpCode == opCode {
			line.Ops = append(line.Ops[:i], line.Ops[i+1:]...)
			i--
		}
	}
}

func (line *LineItem) parseRawText() {
	// consume FEN (up to 4th space)
	var (
		spaces          int
		charsInFENField int
		rest            string
	)

	for i := 0; i < len(line.RawText); i++ {
		if line.RawText[i] == ' ' || (line.RawText[i] == ';' && spaces == 3) {
			spaces++
			charsInFENField = 0
			if spaces == 4 {
				line.FEN = line.RawText[:i]
				rest = line.RawText[i+1:]
				break
			}
		} else {
			charsInFENField++
		}
	}

	if spaces < 4 {
		if spaces == 3 && charsInFENField > 0 {
			line.FEN = line.RawText
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
				line.Ops = append(line.Ops, op)
			}
			continue
		}

		op.Value = strings.TrimSpace(parts[1])
		line.Ops = append(line.Ops, op)
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

func LoadFile(filename string) (*File, error) {
	b, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("file '%s': %v", filename, err)
	}

	return ParseText(string(b)), nil
}

func New() *File {
	return &File{}
}

func ParseText(text string) *File {
	file := New()

	lines := strings.Split(text, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		// skip the last empty line
		if len(line) == 0 && i == len(lines)-1 {
			break
		}
		item := LineItem{RawText: line}
		item.parseRawText()

		file.Lines = append(file.Lines, &item)
	}

	return file
}

func Dedupe(filename string) error {
	file, err := LoadFile(filename)
	if err != nil {
		return err
	}

	seen := make(map[string]int)
	dupes := make(map[string][]int)

	removed := 0
	for i := 0; i < len(file.Lines); i++ {
		line := file.Lines[i]
		if line.FEN == "" {
			continue
		}

		if prevIdx, ok := seen[line.FEN]; !ok {
			seen[line.FEN] = i
		} else {
			if _, ok := dupes[line.FEN]; !ok {
				if line.String() == file.Lines[prevIdx].String() {
					file.Lines = append(file.Lines[:i], file.Lines[i+1:]...)
					i--
					removed++
					continue
				}
				dupes[line.FEN] = append(dupes[line.FEN], prevIdx, i)
			} else {
				prevDupes := dupes[line.FEN]
				found := false
				for j := 0; j < len(prevDupes); j++ {
					prevIdx := prevDupes[j]
					if line.String() == file.Lines[prevIdx].String() {
						file.Lines = append(file.Lines[:i], file.Lines[i+1:]...)
						i--
						removed++
						found = true
						break
					}
				}
				if !found {
					dupes[line.FEN] = append(dupes[line.FEN], i)
				}
			}
		}
	}

	if removed > 0 {
		fmt.Printf("removed %d duplicate(s)\n", removed)
		if err := file.Save(filename, true); err != nil {
			return err
		}
	}

	if len(dupes) == 0 {
		logInfo("no duplicates found")
		return nil
	}

	for _, indexes := range dupes {
		for _, idx := range indexes {
			fmt.Println(file.Lines[idx].String())
		}
	}

	return nil
}

func UpdateFile(ctx context.Context, filename string, opts AnalysisOptions) error {
	analysisOpts := analyze.AnalysisOptions{
		MinDepth:   opts.MinDepth,
		MaxDepth:   opts.MaxDepth,
		MinTime:    opts.MinTime,
		MaxTime:    opts.MaxTime,
		DepthDelta: opts.DepthDelta,
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	file, err := LoadFile(filename)
	if err != nil {
		return err
	}

	tempFilename := filename + ".new"

	if fileExists(tempFilename) {
		return fmt.Errorf("temp file '%s' already exists, please remove or rename it before updating this EPD file", tempFilename)
	}

	filtered := func() []*LineItem {
		var items []*LineItem
		for _, item := range file.Lines {
			if item.FEN == "" || item.ACD() >= 1 {
				continue
			}
			items = append(items, item)
		}
		return items
	}()

	if len(filtered) == 0 {
		return fmt.Errorf("no entries need updating")
	}

	a := analyze.New()

	wg, err := a.StartStockfish(ctx)
	if err != nil {
		return err
	}

	for i := 0; i < len(filtered); i++ {
		item := filtered[i]
		evals, err := a.AnalyzePosition(ctx, analysisOpts, item.FEN)
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
		board := fen.FENtoBoard(item.FEN)
		san := board.UCItoSAN(uci)

		item.SetString(OpCodeBestMove, san)
		item.SetInt(OpCodeAnalysisCountDepth, bestMove.Depth)
		item.SetInt(OpCodeAnalysisCountNodes, bestMove.Nodes)
		item.SetInt(OpCodeAnalysisCountSeconds, bestMove.Time/1000)

		if bestMove.Mate == 0 {
			item.SetInt(OpCodeCentipawnEvaluation, bestMove.POVCP(board.ActiveColor))
			item.Remove(OpCodeDirectMate)
		} else {
			item.SetInt(OpCodeDirectMate, bestMove.POVMate(board.ActiveColor))
			item.Remove(OpCodeCentipawnEvaluation)
		}

		var pvSAN []string
		for _, pvMove := range bestMove.PV {
			pvMoveSAN := board.UCItoSAN(pvMove)
			pvSAN = append(pvSAN, pvMoveSAN)
			board.Moves(pvMove)
		}

		if len(pvSAN) > 1 {
			item.SetString("pm", pvSAN[1])
		}
		if len(pvSAN) > 0 {
			item.SetString("pv", strings.Join(pvSAN, " "))
		}

		if err := file.Save(tempFilename, false); err != nil {
			return err
		}
	}

	if err := file.Save(filename, true); err != nil {
		return err
	}

	if fileExists(tempFilename) {
		if err := os.Remove(tempFilename); err != nil {
			return fmt.Errorf("error remove temp file '%s': %v", tempFilename, err)
		}
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

func fenToKey(fenKey string) string {
	fenKey = strings.TrimSpace(fenKey)
	parts := strings.Split(fenKey, " ")
	if len(parts) <= 4 {
		return fenKey
	}
	return strings.Join(parts[:4], " ")
}

func logInfo(msg string) {
	_, _ = fmt.Fprintf(os.Stderr, "%s\n", strings.TrimRight(msg, "\n"))
}
