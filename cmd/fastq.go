package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/liamg/tml"
	"github.com/spf13/cobra"
)

var fastqCmd = &cobra.Command{
	Use:   "fastq <filename>",
	Short: "Colorize and visualize FASTQ",
	Long:  `Colorize the nucleotides in a FASTQ file and visualize quality scores with block characters`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		renderFASTQ(args[0])
	},
}

func init() {
	rootCmd.AddCommand(fastqCmd)
}

func renderFASTQ(filename string) {
	file, err := os.Open(filename)
	if err != nil {
		fmt.Println("Error opening file:", err)
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineCount := 0

	for scanner.Scan() {
		line := scanner.Text()
		lineCount++

		switch lineCount % 4 {
		case 1: // Sequence ID line
			tml.Printf("<italic>%s</italic>\n", line)
		case 2: // Sequence line
			fmt.Println(colorizeSequence(line))
		case 3: // "+" line
			fmt.Println(line)
		case 0: // Quality score line
			fmt.Println(visualizeQuality(line))
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading file:", err)
	}
}

func colorizeSequence(sequence string) string {
	sequence = strings.ReplaceAll(sequence, "A", tml.Sprintf("<bg-red>A</bg-red>"))
	sequence = strings.ReplaceAll(sequence, "T", tml.Sprintf("<bg-green>T</bg-green>"))
	sequence = strings.ReplaceAll(sequence, "G", tml.Sprintf("<bg-yellow>G</bg-yellow>"))
	sequence = strings.ReplaceAll(sequence, "C", tml.Sprintf("<bg-blue>C</bg-blue>"))
	return sequence
}

func visualizeQuality(quality string) string {
	var sb strings.Builder
	for _, q := range quality {
		score := int(q) - 33 // Convert ASCII to Phred score
		block := getBlockChar(score)
		sb.WriteString(tml.Sprintf("<darkgrey>%s</darkgrey>", block))
	}
	return sb.String()
}

func getBlockChar(score int) string {
	// Use different block characters based on score ranges
	switch {
	case score >= 40:
		return "█"
	case score >= 30:
		return "▓"
	case score >= 20:
		return "▒"
	case score >= 10:
		return "░"
	default:
		return " "
	}
}
