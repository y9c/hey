package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/aquasecurity/table"
	"github.com/spf13/cobra"
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
		rows = append(rows, headers) // Append headers to rows
	}

	// Read up to the first 5 data rows
	var firstFive [][]string
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "\t")
		firstFive = append(firstFive, fields)
		if len(firstFive) == 5 {
			break
		}
	}

	// Seek and find the last 5 rows
	var lastFive [][]string
	if scanner.Scan() { // Check if there's more data beyond the first five
		lastFive = findLastLines(file, 5)
	}

	// Append first five to rows
	rows = append(rows, firstFive...)

	// Check if ellipsis is needed
	if len(firstFive) == 5 && len(lastFive) > 0 {
		ellipsisRow := make([]string, len(headers))
		for i := range ellipsisRow {
			ellipsisRow[i] = "..."
		}
		rows = append(rows, ellipsisRow)
	}

	// Append last five to rows
	rows = append(rows, lastFive...)

	// Create the table
	t := table.New(os.Stdout)
	t.SetHeaders(headers...) // Set the header
	t.SetHeaderStyle(table.StyleBold)
	t.SetLineStyle(table.StyleBlue)
	t.SetDividers(table.UnicodeRoundedDividers)

	// Add rows to the table, skipping the header from addition
	for _, row := range rows[1:] {
		t.AddRow(row...)
	}

	t.Render()
}

func findLastLines(file *os.File, numLines int) [][]string {
	var lines [][]string
	fileSize, _ := file.Seek(0, io.SeekEnd)
	bufSize := 4096
	if fileSize < int64(bufSize) {
		bufSize = int(fileSize)
	}

	buf := make([]byte, bufSize)

	// Start reading from the end
	for position := fileSize; position > 0 && len(lines) < numLines; {
		if position < int64(bufSize) {
			bufSize = int(position)
		}
		position -= int64(bufSize)
		file.Seek(position, io.SeekStart)
		bytesRead, _ := file.Read(buf)
		content := string(buf[:bytesRead])
		tempLines := strings.Split(content, "\n")

		// Process lines in reverse order since we're reading chunks backwards
		for i := len(tempLines) - 1; i >= 0; i-- {
			if tempLines[i] != "" && len(lines) < numLines {
				fields := strings.Split(tempLines[i], "\t")
				lines = append([][]string{fields}, lines...)
			}
		}
	}

	// Ensure not to include any header-like row again
	if len(lines) > numLines {
		lines = lines[len(lines)-numLines:]
	}

	return lines
}
