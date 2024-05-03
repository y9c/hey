package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/aquasecurity/table"
	"github.com/spf13/cobra"
)

var colnameCmd = &cobra.Command{
	Use:   "colname <filename>",
	Short: "Transpose and format table",
	Long:  `Reads all column names but transposes only the first few columns for the first two data rows plus header.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		processTable(args[0])
	},
}

func init() {
	rootCmd.AddCommand(colnameCmd) // Assuming rootCmd is your main command in the application
}

func processTable(filename string) {
	file, err := os.Open(filename)
	if err != nil {
		fmt.Println("Error opening file:", err)
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var transposed [][]string

	// Process header row
	var headers []string
	if scanner.Scan() {
		headers = strings.Split(scanner.Text(), "\t")
		for idx, header := range headers {
			transposed = append(transposed, []string{fmt.Sprintf("%d", idx+1), header}) // Add headers with index
		}
	}

	// Process only the first two data rows
	dataRowCount := 0
	for scanner.Scan() {
		if dataRowCount >= 2 { // Only process two data rows
			break
		}
		row := strings.Split(scanner.Text(), "\t")
		for i := 0; i < len(transposed) && i < len(row); i++ {
			transposed[i] = append(transposed[i], row[i])
		}
		dataRowCount++
	}

	// Create and render the table
	t := table.New(os.Stdout)
	t.SetHeaders("index", "name", "1st", "2nd") // Set headers, assuming only two data rows

	t.SetHeaderStyle(table.StyleBold)
	t.SetLineStyle(table.StyleBlue)
	t.SetDividers(table.UnicodeRoundedDividers)

	// Add rows to the table
	for _, row := range transposed {
		t.AddRow(row...)
	}

	t.Render()
}
