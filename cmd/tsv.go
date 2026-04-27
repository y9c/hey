package cmd

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"image"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

	ui "github.com/gizak/termui/v3"
	"github.com/mattn/go-runewidth"
	"github.com/spf13/cobra"
)

var (
	tsvMaxRows  int
	tsvNoHeader bool
	tsvRowNums  bool
	tsvMaxWidth int
	tsvSep      string
)

var tsvCmd = &cobra.Command{
	Use:   "tsv <filename>",
	Short: "Preview tsv",
	Long:  `Preview tsv file in a pretty way with interactive pager and on-demand loading`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runTSVPager(args[0])
	},
}

func init() {
	rootCmd.AddCommand(tsvCmd)
	tsvCmd.Flags().IntVarP(&tsvMaxRows, "rows", "r", 1000000, "Maximum total rows to allow")
	tsvCmd.Flags().BoolVarP(&tsvNoHeader, "no-header", "H", false, "Hide header row")
	tsvCmd.Flags().BoolVarP(&tsvRowNums, "row-numbers", "n", false, "Show row number column")
	tsvCmd.Flags().IntVarP(&tsvMaxWidth, "max-width", "W", 40, "Maximum column width")
	tsvCmd.Flags().StringVarP(&tsvSep, "sep", "s", "\t", "Column separator (default: tab)")
}

func toSuperscript(num int) string {
	superscripts := []rune("⁰¹²³⁴⁵⁶⁷⁸⁹")
	s := strconv.Itoa(num)
	result := ""
	for _, r := range s {
		result += string(superscripts[r-'0'])
	}
	return result
}

func isNumeric(s string) bool {
	if s == "" || s == "NA" || s == "N/A" || s == "." {
		return false
	}
	_, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", ""), 64)
	return err == nil
}

func formatCommas(s string) string {
	if s == "" {
		return s
	}

	if strings.Contains(strings.ToLower(s), "e") {
		return s
	}

	neg := false
	if s[0] == '-' {
		neg = true
		s = s[1:]
	}

	parts := strings.Split(s, ".")
	intPart := parts[0]

	var result []string
	for i := len(intPart); i > 0; i -= 3 {
		start := i - 3
		if start < 0 {
			start = 0
		}
		result = append([]string{intPart[start:i]}, result...)
	}
	formattedInt := strings.Join(result, ",")

	if neg {
		formattedInt = "-" + formattedInt
	}

	if len(parts) > 1 {
		return formattedInt + "." + parts[1]
	}
	return formattedInt
}

type TsvData struct {
	sync.RWMutex
	Filename    string
	File        *os.File
	GzipReader  *gzip.Reader
	Scanner     *bufio.Scanner
	Headers     []string
	Rows        [][]string
	ColWidths   []int
	IsNumCol    []bool
	FullyLoaded bool
}

func NewTsvData(filename string) (*TsvData, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	var reader io.Reader = file
	var gz *gzip.Reader
	if strings.HasSuffix(filename, ".gz") {
		gz, err = gzip.NewReader(file)
		if err != nil {
			file.Close()
			return nil, err
		}
		reader = gz
	}

	scanner := bufio.NewScanner(reader)
	const maxCapacity = 10 * 1024 * 1024
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxCapacity)

	data := &TsvData{
		Filename:   filename,
		File:       file,
		GzipReader: gz,
		Scanner:    scanner,
	}

	if scanner.Scan() {
		data.Headers = strings.Split(scanner.Text(), tsvSep)
		data.ColWidths = make([]int, len(data.Headers))
		data.IsNumCol = make([]bool, len(data.Headers))
		for i := range data.IsNumCol {
			data.IsNumCol[i] = true
		}
		for i := range data.Headers {
			sw := runewidth.StringWidth(toSuperscript(i + 1))
			data.ColWidths[i] = sw + 4
		}
	} else {
		data.Close()
		return nil, fmt.Errorf("empty or invalid TSV file")
	}

	return data, nil
}

func (d *TsvData) Close() {
	if d.GzipReader != nil {
		d.GzipReader.Close()
	}
	if d.File != nil {
		d.File.Close()
	}
}

func (d *TsvData) LoadMore(n int) int {
	d.Lock()
	defer d.Unlock()

	if d.FullyLoaded {
		return 0
	}

	loaded := 0
	for loaded < n && d.Scanner.Scan() {
		fields := strings.Split(d.Scanner.Text(), tsvSep)
		for len(fields) < len(d.Headers) {
			fields = append(fields, "")
		}
		if len(fields) > len(d.Headers) {
			fields = fields[:len(d.Headers)]
		}

		for i, f := range fields {
			if f != "" && !isNumeric(f) {
				d.IsNumCol[i] = false
			}
			w := runewidth.StringWidth(f)
			if isNumeric(f) {
				w = runewidth.StringWidth(formatCommas(f))
			}
			if w > tsvMaxWidth {
				w = tsvMaxWidth
			}
			if w+2 > d.ColWidths[i] {
				d.ColWidths[i] = w + 2
			}
		}
		d.Rows = append(d.Rows, fields)
		loaded++
	}

	if err := d.Scanner.Err(); err != nil {
		d.FullyLoaded = true
		d.Close()
	} else if loaded < n {
		d.FullyLoaded = true
		d.Close()
	}
	return loaded
}

type TsvPager struct {
	ui.Block
	Data        *TsvData
	RowOffset   int
	ColOffset   int
	ShowHeader  bool
	ShowRowNums bool
}

func NewTsvPager(data *TsvData) *TsvPager {
	p := &TsvPager{
		Block:       *ui.NewBlock(),
		Data:        data,
		ShowHeader:  !tsvNoHeader,
		ShowRowNums: tsvRowNums,
	}
	p.Border = false
	return p
}

// rowNumWidth returns the width needed for the row number column
func (p *TsvPager) rowNumWidth() int {
	if !p.ShowRowNums {
		return 0
	}
	maxRow := len(p.Data.Rows)
	if maxRow < 1 {
		maxRow = 1
	}
	return len(strconv.Itoa(maxRow)) + 2
}

// wrapHeader splits a header into multiple lines to fit a given width.
func wrapHeader(h string, w int) []string {
	if w <= 0 {
		return []string{h}
	}
	delimiters := []string{"_", " ", "-", "."}
	var words []string

	tempH := h
	for {
		found := false
		firstIdx := len(tempH)
		firstDelim := ""
		for _, d := range delimiters {
			idx := strings.Index(tempH, d)
			if idx != -1 && idx < firstIdx {
				firstIdx = idx
				firstDelim = d
				found = true
			}
		}
		if !found {
			words = append(words, tempH)
			break
		}
		words = append(words, tempH[:firstIdx+len(firstDelim)])
		tempH = tempH[firstIdx+len(firstDelim):]
		if tempH == "" {
			break
		}
	}

	var lines []string
	currLine := ""
	for _, word := range words {
		if runewidth.StringWidth(currLine+word) <= w {
			currLine += word
		} else {
			if currLine != "" {
				lines = append(lines, currLine)
			}
			for runewidth.StringWidth(word) > w {
				part := runewidth.Truncate(word, w, "")
				lines = append(lines, part)
				word = word[len(part):]
			}
			currLine = word
		}
	}
	if currLine != "" {
		lines = append(lines, currLine)
	}
	return lines
}

func (p *TsvPager) Draw(buf *ui.Buffer) {
	p.Block.Draw(buf)
	p.Data.RLock()
	defer p.Data.RUnlock()

	borderStyle := ui.NewStyle(ui.ColorBlue)
	ellipsis := ">"

	truncate := func(s string, w int) string {
		sw := runewidth.StringWidth(s)
		if sw <= w {
			return s + strings.Repeat(" ", w-sw)
		}
		return runewidth.Truncate(s, w-1, "...") + " "
	}

	alignRight := func(s string, w int) string {
		sw := runewidth.StringWidth(s)
		if sw >= w {
			return truncate(s, w)
		}
		return strings.Repeat(" ", w-sw-1) + s + " "
	}

	drawLine := func(y int, left, middle, right, horizontal string, widths []int, startIdx int) {
		currX := p.Inner.Min.X
		if currX >= p.Inner.Max.X {
			return
		}
		buf.SetString(left, borderStyle, image.Pt(currX, y))
		currX++
		for i := startIdx; i < len(widths); i++ {
			w := widths[i]
			if currX+w >= p.Inner.Max.X {
				remaining := p.Inner.Max.X - currX - 1
				if remaining > 0 {
					buf.SetString(strings.Repeat(horizontal, remaining)+ellipsis, borderStyle, image.Pt(currX, y))
				} else if remaining == 0 {
					buf.SetString(ellipsis, borderStyle, image.Pt(currX, y))
				}
				return
			}
			buf.SetString(strings.Repeat(horizontal, w), borderStyle, image.Pt(currX, y))
			currX += w
			if i == len(widths)-1 {
				if currX < p.Inner.Max.X {
					buf.SetString(right, borderStyle, image.Pt(currX, y))
				}
			} else {
				if currX < p.Inner.Max.X {
					buf.SetString(middle, borderStyle, image.Pt(currX, y))
				}
			}
			currX++
		}
	}

	drawCellBorder := func(y int, currX int) int {
		if currX < p.Inner.Max.X {
			buf.SetString("│", borderStyle, image.Pt(currX, y))
		}
		return currX + 1
	}

	y := p.Inner.Min.Y

	// Build drawWidths with only visible columns (row num + data columns from ColOffset)
	drawWidths := make([]int, 0)
	if p.ShowRowNums {
		drawWidths = append(drawWidths, p.rowNumWidth())
	}
	for i := p.ColOffset; i < len(p.Data.ColWidths); i++ {
		drawWidths = append(drawWidths, p.Data.ColWidths[i])
	}

	// Top border with round corners
	drawLine(y, "╭", "┬", "╮", "─", drawWidths, 0)
	y++

	// Header section
	if p.ShowHeader {
		headerLines := make([][]string, len(p.Data.Headers))
		maxHeaderHeight := 1
		for i := p.ColOffset; i < len(p.Data.Headers); i++ {
			w := p.Data.ColWidths[i] - 1
			lines := wrapHeader(p.Data.Headers[i], w)
			headerLines[i] = lines
			if len(lines) > maxHeaderHeight {
				maxHeaderHeight = len(lines)
			}
		}

		// Render multi-line header
		for hLine := 0; hLine < maxHeaderHeight && y < p.Inner.Max.Y; hLine++ {
			currX := p.Inner.Min.X
			currX = drawCellBorder(y, currX)

			// Row number header cell
			if p.ShowRowNums {
				rw := p.rowNumWidth()
				if currX+rw >= p.Inner.Max.X {
					buf.SetString(ellipsis, ui.NewStyle(ui.ColorRed), image.Pt(p.Inner.Max.X-1, y))
				} else {
					style := ui.NewStyle(ui.ColorYellow, ui.ColorClear, ui.ModifierBold)
					val := ""
					if hLine == 0 {
						val = "#"
					}
					buf.SetString(truncate(val, rw), style, image.Pt(currX, y))
					currX += rw
					currX = drawCellBorder(y, currX)
				}
			}

			for i := p.ColOffset; i < len(p.Data.Headers); i++ {
				w := p.Data.ColWidths[i]
				if currX+w >= p.Inner.Max.X {
					buf.SetString(ellipsis, ui.NewStyle(ui.ColorRed), image.Pt(p.Inner.Max.X-1, y))
					break
				}

				style := ui.NewStyle(ui.ColorCyan, ui.ColorClear, ui.ModifierBold)
				val := ""
				if hLine < len(headerLines[i]) {
					val = headerLines[i][hLine]
				}

				if hLine == 0 {
					ss := toSuperscript(i + 1)
					if runewidth.StringWidth(val)+runewidth.StringWidth(ss) <= w-1 {
						val += ss
					}
				}

				buf.SetString(truncate(val, w), style, image.Pt(currX, y))
				currX += w
				currX = drawCellBorder(y, currX)
			}
			y++
		}

		if y < p.Inner.Max.Y {
			drawLine(y, "├", "┼", "┤", "─", drawWidths, 0)
			y++
		}
	}

	// Data rows
	lastIdx := -1
	for i := p.RowOffset; i < len(p.Data.Rows) && y < p.Inner.Max.Y-1; i++ {
		currX := p.Inner.Min.X
		currX = drawCellBorder(y, currX)

		// Row number cell
		if p.ShowRowNums {
			rw := p.rowNumWidth()
			if currX+rw >= p.Inner.Max.X {
				buf.SetString(ellipsis, ui.NewStyle(ui.ColorRed), image.Pt(p.Inner.Max.X-1, y))
				lastIdx = i
				y++
				continue
			}
			rowNum := strconv.Itoa(i + 1)
			style := ui.NewStyle(ui.ColorYellow)
			buf.SetString(alignRight(rowNum, rw), style, image.Pt(currX, y))
			currX += rw
			currX = drawCellBorder(y, currX)
		}

		for colIdx := p.ColOffset; colIdx < len(p.Data.Rows[i]); colIdx++ {
			w := p.Data.ColWidths[colIdx]
			if currX+w >= p.Inner.Max.X {
				buf.SetString(ellipsis, ui.NewStyle(ui.ColorRed), image.Pt(p.Inner.Max.X-1, y))
				break
			}

			val := p.Data.Rows[i][colIdx]
			style := ui.NewStyle(ui.ColorWhite)

			if p.Data.IsNumCol[colIdx] {
				if isNumeric(val) {
					val = formatCommas(val)
					style = ui.NewStyle(ui.ColorGreen)
				}
				buf.SetString(alignRight(val, w), style, image.Pt(currX, y))
			} else {
				buf.SetString(truncate(val, w), style, image.Pt(currX, y))
			}

			currX += w
			currX = drawCellBorder(y, currX)
		}

		lastIdx = i
		y++
		if y < p.Inner.Max.Y-1 {
			if i < len(p.Data.Rows)-1 {
				drawLine(y, "├", "┼", "┤", "─", drawWidths, 0)
				y++
			}
		}
	}

	if lastIdx == len(p.Data.Rows)-1 && p.Data.FullyLoaded && y < p.Inner.Max.Y-1 {
		drawLine(y, "╰", "┴", "╯", "─", drawWidths, 0)
	}

	// Status bar
	headerState := "ON"
	if !p.ShowHeader {
		headerState = "OFF"
	}
	rowNumState := "OFF"
	if p.ShowRowNums {
		rowNumState = "ON"
	}
	status := fmt.Sprintf(" [Row %d/%d, Col %d/%d] [H]Header:%s [N]RowNum:%s [q]Quit ",
		p.RowOffset+1, len(p.Data.Rows), p.ColOffset+1, len(p.Data.Headers),
		headerState, rowNumState)
	if !p.Data.FullyLoaded {
		status = fmt.Sprintf(" [Row %d/%d+, Col %d/%d] [H]Header:%s [N]RowNum:%s (Loading...) ",
			p.RowOffset+1, len(p.Data.Rows), p.ColOffset+1, len(p.Data.Headers),
			headerState, rowNumState)
	}
	buf.SetString(status, ui.NewStyle(ui.ColorBlack, ui.ColorWhite), image.Pt(p.Inner.Min.X, p.Max.Y-1))
}

func runTSVPager(filename string) {
	data, err := NewTsvData(filename)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer data.Close()

	if err := ui.Init(); err != nil {
		fmt.Printf("failed to initialize termui: %v", err)
		return
	}
	defer ui.Close()

	data.LoadMore(200)

	pager := NewTsvPager(data)
	termWidth, termHeight := ui.TerminalDimensions()
	pager.SetRect(0, 0, termWidth, termHeight)

	ui.Render(pager)

	uiEvents := ui.PollEvents()
	for {
		select {
		case e := <-uiEvents:
			switch e.ID {
			case "q", "<C-c>":
				return
			case "H":
				pager.ShowHeader = !pager.ShowHeader
				ui.Render(pager)
			case "N":
				pager.ShowRowNums = !pager.ShowRowNums
				ui.Render(pager)
			case "<Up>", "k":
				if pager.RowOffset > 0 {
					pager.RowOffset--
					ui.Render(pager)
				}
			case "<Down>", "j":
				data.RLock()
				numRows := len(data.Rows)
				data.RUnlock()
				if pager.RowOffset+(termHeight/2) >= numRows && !data.FullyLoaded {
					data.LoadMore(200)
					numRows = len(data.Rows)
				}
				if pager.RowOffset < numRows-1 {
					pager.RowOffset++
					ui.Render(pager)
				}
			case "<Left>", "h":
				if pager.ColOffset > 0 {
					pager.ColOffset--
					ui.Render(pager)
				}
			case "<Right>", "l":
				if pager.ColOffset < len(data.Headers)-1 {
					pager.ColOffset++
					ui.Render(pager)
				}
			case "<PageUp>":
				pager.RowOffset -= termHeight / 4
				if pager.RowOffset < 0 {
					pager.RowOffset = 0
				}
				ui.Render(pager)
			case "<PageDown>":
				data.RLock()
				numRows := len(data.Rows)
				data.RUnlock()
				for pager.RowOffset+(termHeight/2) >= numRows && !data.FullyLoaded {
					data.LoadMore(200)
					numRows = len(data.Rows)
				}
				pager.RowOffset += termHeight / 4
				if pager.RowOffset > numRows-1 {
					pager.RowOffset = numRows - 1
				}
				if pager.RowOffset < 0 {
					pager.RowOffset = 0
				}
				ui.Render(pager)
			case "<Home>":
				pager.RowOffset = 0
				ui.Render(pager)
			case "<End>":
				for !data.FullyLoaded {
					data.LoadMore(2000)
				}
				pager.RowOffset = len(data.Rows) - 1
				if pager.RowOffset < 0 {
					pager.RowOffset = 0
				}
				ui.Render(pager)
			case "<Resize>":
				payload := e.Payload.(ui.Resize)
				termWidth, termHeight = payload.Width, payload.Height
				pager.SetRect(0, 0, termWidth, termHeight)
				ui.Render(pager)
			}
		}
	}
}
