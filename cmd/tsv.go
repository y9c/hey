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
	tsvMaxRows int
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
	data := &TsvData{
		Filename:   filename,
		File:       file,
		GzipReader: gz,
		Scanner:    scanner,
	}

	// Load headers immediately
	if scanner.Scan() {
		data.Headers = strings.Split(scanner.Text(), "\t")
		data.ColWidths = make([]int, len(data.Headers))
		data.IsNumCol = make([]bool, len(data.Headers))
		for i := range data.IsNumCol {
			data.IsNumCol[i] = true
		}
		for i, h := range data.Headers {
			w := runewidth.StringWidth(h) + runewidth.StringWidth(toSuperscript(i+1))
			if w > 40 {
				w = 40
			}
			data.ColWidths[i] = w + 2 // Initial padding
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
		fields := strings.Split(d.Scanner.Text(), "\t")
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
			if w > 40 {
				w = 40
			}
			if w+2 > d.ColWidths[i] {
				d.ColWidths[i] = w + 2
			}
		}
		d.Rows = append(d.Rows, fields)
		loaded++
	}

	if loaded < n {
		d.FullyLoaded = true
		d.Close()
	}
	return loaded
}

type TsvPager struct {
	ui.Block
	Data      *TsvData
	RowOffset int
	ColOffset int
}

func NewTsvPager(data *TsvData) *TsvPager {
	p := &TsvPager{
		Block: *ui.NewBlock(),
		Data:  data,
	}
	p.Border = false
	return p
}

func (self *TsvPager) Draw(buf *ui.Buffer) {
	self.Block.Draw(buf)
	self.Data.RLock()
	defer self.Data.RUnlock()

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

	drawLine := func(y int, left, middle, right, horizontal string, widths []int, startCol int) {
		currX := self.Inner.Min.X
		buf.SetString(left, ui.NewStyle(ui.ColorWhite), image.Pt(currX, y))
		currX++
		for i := startCol; i < len(widths); i++ {
			w := widths[i]
			if currX+w >= self.Inner.Max.X {
				buf.SetString(strings.Repeat(horizontal, self.Inner.Max.X-currX-1)+">", ui.NewStyle(ui.ColorWhite), image.Pt(currX, y))
				return
			}
			buf.SetString(strings.Repeat(horizontal, w), ui.NewStyle(ui.ColorWhite), image.Pt(currX, y))
			currX += w
			if i == len(widths)-1 {
				buf.SetString(right, ui.NewStyle(ui.ColorWhite), image.Pt(currX, y))
			} else {
				buf.SetString(middle, ui.NewStyle(ui.ColorWhite), image.Pt(currX, y))
			}
			currX++
		}
	}

	drawRow := func(y int, fields []string, widths []int, startCol int, isHeader bool) {
		currX := self.Inner.Min.X
		buf.SetString("┃", ui.NewStyle(ui.ColorWhite), image.Pt(currX, y))
		currX++

		for i := startCol; i < len(fields); i++ {
			w := widths[i]
			if currX+w >= self.Inner.Max.X {
				buf.SetString(">", ui.NewStyle(ui.ColorRed), image.Pt(self.Inner.Max.X-1, y))
				break
			}

			val := fields[i]
			style := ui.NewStyle(ui.ColorWhite)

			if isHeader {
				val = val + toSuperscript(i+1)
				style = ui.NewStyle(ui.ColorCyan, ui.ColorClear, ui.ModifierBold)
				buf.SetString(truncate(val, w), style, image.Pt(currX, y))
			} else {
				if self.Data.IsNumCol[i] {
					if isNumeric(val) {
						val = formatCommas(val)
						style = ui.NewStyle(ui.ColorGreen)
					}
					buf.SetString(alignRight(val, w), style, image.Pt(currX, y))
				} else {
					buf.SetString(truncate(val, w), style, image.Pt(currX, y))
				}
			}

			currX += w
			buf.SetString("┃", ui.NewStyle(ui.ColorWhite), image.Pt(currX, y))
			currX++
		}
	}

	y := self.Inner.Min.Y
	drawLine(y, "┏", "┳", "┓", "━", self.Data.ColWidths, self.ColOffset)
	y++

	if y < self.Inner.Max.Y {
		drawRow(y, self.Data.Headers, self.Data.ColWidths, self.ColOffset, true)
		y++
	}

	if y < self.Inner.Max.Y {
		drawLine(y, "┣", "╋", "┫", "━", self.Data.ColWidths, self.ColOffset)
		y++
	}

	lastIdx := -1
	for i := self.RowOffset; i < len(self.Data.Rows) && y < self.Inner.Max.Y-1; i++ {
		drawRow(y, self.Data.Rows[i], self.Data.ColWidths, self.ColOffset, false)
		lastIdx = i
		y++
		if y < self.Inner.Max.Y-1 {
			if i < len(self.Data.Rows)-1 {
				drawLine(y, "┣", "╋", "┫", "━", self.Data.ColWidths, self.ColOffset)
				y++
			}
		}
	}

	if lastIdx == len(self.Data.Rows)-1 && self.Data.FullyLoaded && y < self.Inner.Max.Y-1 {
		drawLine(y, "┗", "┻", "┛", "━", self.Data.ColWidths, self.ColOffset)
	}

	status := fmt.Sprintf(" [Row %d/%d, Col %d/%d] ('q' to quit) ", self.RowOffset+1, len(self.Data.Rows), self.ColOffset+1, len(self.Data.Headers))
	if !self.Data.FullyLoaded {
		status = fmt.Sprintf(" [Row %d/%d+, Col %d/%d] (Loading...) ", self.RowOffset+1, len(self.Data.Rows), self.ColOffset+1, len(self.Data.Headers))
	}
	buf.SetString(status, ui.NewStyle(ui.ColorBlack, ui.ColorWhite), image.Pt(self.Inner.Min.X, self.Max.Y-1))
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

	data.LoadMore(100) // Initial load

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
			case "<Up>", "k":
				if pager.RowOffset > 0 {
					pager.RowOffset--
					ui.Render(pager)
				}
			case "<Down>", "j":
				data.RLock()
				numRows := len(data.Rows)
				data.RUnlock()
				if pager.RowOffset+1 >= numRows && !data.FullyLoaded {
					data.LoadMore(100)
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
				for pager.RowOffset+termHeight/4 >= numRows && !data.FullyLoaded {
					data.LoadMore(100)
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
				// For <End>, we have to load everything to know where the end is
				for !data.FullyLoaded {
					data.LoadMore(1000)
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
