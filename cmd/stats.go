package cmd

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/aquasecurity/table"
	"github.com/liamg/tml" // Use the tml package for colored output
	"github.com/spf13/cobra"
)

var (
	statsSeparator string
	scaleToK       bool // Flag for "per thousand"
	scaleToM       bool // Flag for "per million"
	statsCmd       = &cobra.Command{
		Use:   "stats [filenames...]",
		Short: "Concatenate first two columns from files and transpose into a matrix",
		Long: `Reads one or more files, extracts the first two columns, concatenates
the data into a single dataset, and transposes it into a matrix where the filenames
are column headers, the first column is row indices, and the second column is the value.
Supports scaling to 'per thousand' (-k), 'per million' (-m), or formatting with commas.`,
		Args: cobra.MinimumNArgs(1), // Requires at least one filename
		Run: func(cmd *cobra.Command, args []string) {
			transposeMatrix(args)
		},
	}
)

func init() {
	rootCmd.AddCommand(statsCmd)
	statsCmd.Flags().StringVarP(&statsSeparator, "separator", "s", "\t", "Column separator (default is tab)")
	statsCmd.Flags().BoolVarP(&scaleToK, "per-thousand", "k", false, "Scale numbers to 'per thousand' (append 'k')")
	statsCmd.Flags().BoolVarP(&scaleToM, "per-million", "m", false, "Scale numbers to 'per million' (append 'M')")
}

func transposeMatrix(filenames []string) {
	data := make(map[string]map[string]string) // Map[rowKey][fileName] = value
	var rowKeys []string                       // Slice to track row keys in their first occurrence order
	rowKeySeen := make(map[string]bool)        // Map to track if a row key has been seen

	// Read and process each file
	for _, fileName := range filenames {
		var input io.Reader

		if fileName == "-" {
			input = os.Stdin
		} else {
			file, err := os.Open(fileName)
			if err != nil {
				fmt.Printf("Error opening file %s: %v\n", fileName, err)
				return
			}
			defer file.Close()

			if strings.HasSuffix(fileName, ".gz") {
				gzipReader, err := gzip.NewReader(file)
				if err != nil {
					fmt.Printf("Error opening gzip file %s: %v\n", fileName, err)
					return
				}
				defer gzipReader.Close()
				input = gzipReader
			} else {
				input = file
			}
		}

		scanner := bufio.NewScanner(input)
		data[fileName] = make(map[string]string)
		for scanner.Scan() {
			columns := strings.Split(scanner.Text(), statsSeparator)
			if len(columns) >= 2 {
				rowKey := columns[0]
				value := columns[1]
				value = formatValue(value)
				data[fileName][rowKey] = value

				// Add rowKey to rowKeys slice if it's the first time we've seen it
				if !rowKeySeen[rowKey] {
					rowKeys = append(rowKeys, rowKey)
					rowKeySeen[rowKey] = true
				}
			}
		}
	}

	// Print transposed table
	t := table.New(os.Stdout)
	headers := append([]string{""}, filenames...)
	for i := range headers {
		headers[i] = tml.Sprintf("<blue>%s</blue>", headers[i]) // Apply blue color without numbering
	}
	t.SetHeaders(headers...)
	t.SetHeaderStyle(table.StyleBold)
	t.SetLineStyle(table.StyleBlue)
	t.SetDividers(table.UnicodeRoundedDividers)

	for _, rowKey := range rowKeys { // Use rowKeys slice to maintain order
		row := []string{rowKey}
		for _, fileName := range filenames {
			if val, exists := data[fileName][rowKey]; exists {
				row = append(row, tml.Sprintf("<green>%s</green>", val)) // Apply green color
			} else {
				row = append(row, "N/A") // Fill missing values
			}
		}
		t.AddRow(row...)
	}
	t.Render()
}

func formatValue(value string) string {
	if num, err := strconv.ParseFloat(value, 64); err == nil {
		if scaleToK {
			return fmt.Sprintf("%.1fk", num/1000) // Scale to per thousand
		} else if scaleToM {
			return fmt.Sprintf("%.1fM", num/1e6) // Scale to per million
		} else {
			return formatWithCommas(int(num)) // Default: add commas
		}
	}
	return value
}

func formatWithCommas(num int) string {
	str := strconv.Itoa(num)
	n := len(str)
	if n <= 3 {
		return str
	}
	var result strings.Builder
	for i, c := range str {
		if (n-i)%3 == 0 && i > 0 {
			result.WriteRune(',')
		}
		result.WriteRune(c)
	}
	return result.String()
}
