package cmd

import (
	"github.com/spf13/cobra"

	"bufio"
	"compress/gzip"
	"fmt"
	"os"
	"strings"
)

var inputFile string

var (
	wcCmd = &cobra.Command{
		Use:   "wc",
		Short: "Count line number (gzip suportted, multiple files support)",
		Long:  `Better than linux build-in wc and gzip format will be supported`,
		Run: func(cmd *cobra.Command, args []string) {
			countLines(args)
		},
	}
)

func init() {
	rootCmd.AddCommand(wcCmd)
	// wcCmd.Flags().StringVarP(&inputFile, "input", "i", "", "input name list file")
	// wcCmd.MarkFlagRequired("input")
}

func countLines(filePaths []string) {
	for _, filePath := range filePaths {
		file, err := os.Open(filePath)
		if err != nil {
			fmt.Println("Error opening file:", err)
			return
		}
		defer file.Close()

		var reader *bufio.Reader
		if strings.HasSuffix(file.Name(), ".gz") {
			gzReader, err := gzip.NewReader(file)
			if err != nil {
				fmt.Println("Error creating gzip reader:", err)
				return
			}
			defer gzReader.Close()
			reader = bufio.NewReader(gzReader)
		} else {
			reader = bufio.NewReader(file)
		}

		scanner := bufio.NewScanner(reader)
		scanner.Split(bufio.ScanLines)

		lineCount := 0
		for scanner.Scan() {
			lineCount++
		}

		if err := scanner.Err(); err != nil {
			fmt.Println("Error scanning file:", err)
			return
		}

		fmt.Printf("%s\t%d\n", filePath, lineCount)
	}
}
