package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var dnaRcCmd = &cobra.Command{
	Use:   "rc [filename]",
	Short: "Compute the reverse complement of DNA sequences",
	Long: `Reads DNA sequences from stdin or a specified file and outputs the reverse complement of each sequence,
handling standard and ambiguous bases.`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var input io.Reader = os.Stdin
		if len(args) == 1 && args[0] != "-" {
			file, err := os.Open(args[0])
			if err != nil {
				fmt.Println("Error opening file:", err)
				return
			}
			defer file.Close()
			input = file
		}
		processSequences(input)
	},
}

func init() {
	rootCmd.AddCommand(dnaRcCmd)
}

func processSequences(input io.Reader) {
	complements := map[rune]rune{
		'A': 'T', 'T': 'A', 'C': 'G', 'G': 'C',
		'M': 'K', 'K': 'M', 'R': 'Y', 'Y': 'R',
		'W': 'W', 'S': 'S', 'B': 'V', 'V': 'B',
		'D': 'H', 'H': 'D', 'N': 'N',
	}

	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		sequence := scanner.Text()
		reverseComp := reverseComplement(sequence, complements)
		fmt.Println(reverseComp)
	}
}

func reverseComplement(sequence string, complements map[rune]rune) string {
	var revComp strings.Builder
	revComp.Grow(len(sequence))
	for i := len(sequence) - 1; i >= 0; i-- {
		if comp, exists := complements[rune(sequence[i])]; exists {
			revComp.WriteRune(comp)
		} else {
			revComp.WriteRune('N') // Default for unrecognized characters
		}
	}
	return revComp.String()
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
