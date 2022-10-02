package cmd

import (
	"encoding/csv"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var colnameCmd = &cobra.Command{
	Use:   "colname",
	Short: "Show column name and first 3 rows into a tidy table",
	Long:  `Don't use your pen to point at my screen anymore!!!`,
	Run: func(cmd *cobra.Command, args []string) {
		showColname("Hello!")
	},
}

func init() {
	rootCmd.AddCommand(colnameCmd)
}

func showColname(content string) {
	fmt.Println(content)
	csvFile, err := os.Open("./tabdata.csv")

	if err != nil {
		fmt.Println(err)
	}

	defer csvFile.Close()

	reader := csv.NewReader(csvFile)

	reader.Comma = '\t' // Use tab-delimited instead of comma <---- here!

	reader.FieldsPerRecord = -1

	csvData, err := reader.ReadAll()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	for _, each := range csvData {
		fmt.Println("...", each)
	}
}
