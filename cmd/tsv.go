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

	// Read the headers
	if scanner.Scan() {
		headers = strings.Split(scanner.Text(), "\t")
		for i, header := range headers {
			// headers[i] = tml.Sprintf("<blue>%s</blue><red>%s</red>", header, toSuperscript(i+1))
			headers[i] = tml.Sprintf("<blue>%s</blue>", header) + toSuperscript(i+1)
		}
		rows = append(rows, headers) // Append processed headers to rows
	}

	// Read and process data rows
	var firstRows [][]string
	var additionalRowScanned bool
	halfRows := maxRows / 2
	overflow := maxRows % 2

	for i := 0; scanner.Scan() && i < halfRows+overflow; i++ {
		fields := strings.Split(scanner.Text(), "\t")
		firstRows = append(firstRows, fields)
	}

	additionalRowScanned = scanner.Scan()
	var lastRows [][]string
	if additionalRowScanned {
		lastRows = findLastLines(file, halfRows)
	}

	rows = append(rows, firstRows...)
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
