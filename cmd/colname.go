package cmd

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/aquasecurity/table"
	"github.com/spf13/cobra"
)

var colnameCmd = &cobra.Command{
	Use:   "colname [filename]",
	Short: "Transpose and format table",
	Long:  `Reads column names and transposes only the first few columns for the first two data rows plus header from a file or stdin. Supports gzip.`,
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var input io.Reader = os.Stdin
		if len(args) == 1 && args[0] != "-" {
			file, err := os.Open(args[0])
			if err != nil {
				fmt.Println("Error opening file:", err)
				return
			}
			defer file.Close()
			if strings.HasSuffix(args[0], ".gz") {
				gzipReader, err := gzip.NewReader(file)
				if err != nil {
					fmt.Println("Error opening gzip file:", err)
					return
				}
				defer gzipReader.Close()
				input = gzipReader
			} else {
				input = file
			}
		}
		processTable(input)
	},
}

func init() {
	rootCmd.AddCommand(colnameCmd)
}

func processTable(input io.Reader) {
	scanner := bufio.NewScanner(input)
	var transposed [][]string
	var headers []string

	if scanner.Scan() {
		headers = strings.Split(scanner.Text(), "\t")
		for idx, header := range headers {
			transposed = append(transposed, []string{fmt.Sprintf("%d", idx+1), header})
		}
	}

	dataRowCount := 0
	for scanner.Scan() {
		if dataRowCount >= 2 {
			break
		}
		row := strings.Split(scanner.Text(), "\t")
		for i := 0; i < len(transposed) && i < len(row); i++ {
			transposed[i] = append(transposed[i], row[i])
		}
		dataRowCount++
	}

	t := table.New(os.Stdout)
	t.SetHeaders("index", "name", "1st", "2nd")
	t.SetHeaderStyle(table.StyleBold)
	t.SetLineStyle(table.StyleBlue)
	t.SetDividers(table.UnicodeRoundedDividers)

	for _, row := range transposed {
		t.AddRow(row...)
	}
	t.Render()
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
