package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/aquasecurity/table"
	"github.com/liamg/tml"
	"github.com/spf13/cobra"
)

var (
	maxRows    int
	maxColumns int
)

var tsvCmd = &cobra.Command{
	Use:   "tsv <filename>",
	Short: "Preview tsv",
	Long:  `Preview tsv file in a pretty way`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		renderTable(args[0])
	},
}

func init() {
	rootCmd.AddCommand(tsvCmd)
	tsvCmd.Flags().IntVarP(&maxRows, "rows", "r", 10, "Maximum number of rows to display")
	tsvCmd.Flags().IntVarP(&maxColumns, "columns", "c", 10, "Maximum number of columns to display")
}

func toSuperscript(num int) string {
	superscripts := []string{"⁰", "¹", "²", "³", "⁴", "⁵", "⁶", "⁷", "⁸", "⁹"}
	result := ""
	for num > 0 {
		digit := num % 10
		result = superscripts[digit] + result
		num /= 10
	}
	return result
}

func processColumns(fields []string, maxColumns int) []string {
	if len(fields) <= maxColumns {
		return fields
	}
	halfColumns := maxColumns / 2
	overflow := maxColumns % 2
	firstPart := fields[:halfColumns+overflow]
	lastPart := fields[len(fields)-halfColumns:]
	middle := []string{"..."} // Ellipsis with no color
	return append(append(firstPart, middle...), lastPart...)
}

func renderTable(filename string) {
	file, err := os.Open(filename)
	if err != nil {
		fmt.Println("Error opening file:", err)
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var rows [][]string
	var headers []string

	// Read and color the headers
	if scanner.Scan() {
		headers = strings.Split(scanner.Text(), "\t")
		for i, header := range headers {
			headers[i] = tml.Sprintf("<blue>%s</blue>", header) + toSuperscript(i+1)
		}
		processedHeaders := processColumns(headers, maxColumns)
		rows = append(rows, processedHeaders)
	}

	var firstRows [][]string
	var additionalRowScanned bool
	halfRows := maxRows / 2
	overflow := maxRows % 2

	for i := 0; scanner.Scan() && i < halfRows+overflow; i++ {
		fields := strings.Split(scanner.Text(), "\t")
		firstRows = append(firstRows, processColumns(fields, maxColumns))
	}

	additionalRowScanned = scanner.Scan()
	var lastRows [][]string
	if additionalRowScanned {
		lastRows = findLastLines(file, halfRows)
		for i := range lastRows {
			lastRows[i] = processColumns(lastRows[i], maxColumns)
		}
	}

	rows = append(rows, firstRows...)

	// Add an ellipsis row if needed
	if additionalRowScanned && len(lastRows) > 0 {
		ellipsisRow := make([]string, len(rows[0]))
		for i := range ellipsisRow {
			ellipsisRow[i] = "..."
		}
		rows = append(rows, ellipsisRow)
	}

	rows = append(rows, lastRows...)

	t := table.New(os.Stdout)
	t.SetHeaders(rows[0]...)
	t.SetHeaderStyle(table.StyleBold)
	t.SetLineStyle(table.StyleBlue)
	t.SetDividers(table.UnicodeRoundedDividers)

	for _, row := range rows[1:] {
		t.AddRow(row...)
	}

	t.Render()
}

func findLastLines(file *os.File, numLines int) [][]string {
	var lines [][]string
	bufSize := 4096
	fileSize, _ := file.Seek(0, io.SeekEnd)
	buf := make([]byte, bufSize)

	for position := fileSize; position > 0 && len(lines) < numLines; {
		if position < int64(bufSize) {
			bufSize = int(position)
		}
		position -= int64(bufSize)
		file.Seek(position, io.SeekStart)
		bytesRead, _ := file.Read(buf)
		content := string(buf[:bytesRead])
		tempLines := strings.Split(content, "\n")

		for i := len(tempLines) - 1; i >= 0; i-- {
			if tempLines[i] != "" && len(lines) < numLines {
				fields := strings.Split(tempLines[i], "\t")
				lines = append([][]string{fields}, lines...)
			}
		}
	}

	return lines
}
