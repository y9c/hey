package cmd

import (
	"bufio"
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"
)

// testCmd represents the test command
var paperCmd = &cobra.Command{
	Use:   "paper",
	Short: "random HeLab member for next week Journal Club",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:`,

	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Paper called")
		fmt.Println("============")
		lines, err := readLines("member.txt")
		if err != nil {
			log.Fatalf("readLines: %s", err)
		}
		randomMember(lines)

	},
}

func init() {
	rootCmd.AddCommand(paperCmd)
}

func readLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func randomMember(s []string) {
	for i, line := range s {
		fmt.Println(i, line)
	}
}
