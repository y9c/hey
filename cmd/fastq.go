package cmd

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/liamg/tml"
	"github.com/spf13/cobra"
)

var adapterSequences = map[string]string{
	"AATGATACGGCGACCACCGAGATCTACACTCTTTCCCTACACGACGCTCTTCCGATCT":    "Illumina Single End PCR Primer 1",
	"ACAGGTTCAGAGTTCTACAGTCCGAC":                                    "Illumina DpnII expression Adapter 1",
	"CAAGCAGAAGACGGCATACGAGATCGGTCTCGGCATTCCTGCTGAACCGCTCTTCCGATCT": "Illumina Paired End PCR Primer 2",
	"CAAGCAGAAGACGGCATACGAGCTCTTCCGATCT":                            "Illumina Single End Adapter 2",
	"CCACTACGCCTCCGCTTTCCTCTCTATGGGCAGTCGGTGAT":                     "ABI Solid3 Adapter B",
	"CGACAGGTTCAGAGTTCTACAGTCCGACGATC":                              "Illumina 5p RNA Adapter",
	"CGGTCTCGGCATTCCTGCTGAACCGCTCTTCCGATCT":                         "Illumina Paired End Sequencing Primer 2",
	"CTGATCTAGAGGTACCGGATCCCAGCAGT":                                 "ABI Dynabead EcoP Oligo",
	"CTGCCCCGGGTTCCTCATTCTCTCAGCAGCATG":                             "ABI Solid3 Adapter A",
	"GATCGGAAGAGCACACGTCTGAACTCCAGTCAC":                             "Illumina Multiplexing Adapter 1",
	"GATCGGAAGAGCGGTTCAGCAGGAATGCCGAG":                              "Illumina Paired End Adapter 2",
	"GATCGGAAGAGCTCGTATGCCGTCTTCTGCTTG":                             "Illumina Single End Adapter 1",
	"GTGACTGGAGTTCAGACGTGTGCTCTTCCGATCT":                            "TruSeq Multiplexing Read 2 Sequencing Primer",
	"TGGAATTCTCGGGTGCCAAGG":                                         "Illumina Small RNA 3p Adapter 1",
	"CTGTCTCTTATACACATCT":                                           "Tn5 ME Adapter",
	"AGATCGGAAGAGCGTCGTGTAGGGAAAGAGTGT":                             "TruSeq P5 Adapter",
	"GATCGTCGGACTGTAGAACTCTGAAC":                                    "Samll RNA P5 Adapter",
}

// fastqCmd represents the fastq command
var fastqCmd = &cobra.Command{
	Use:   "fastq [filename]",
	Short: "Colorize and visualize FASTQ",
	Long:  `Colorize the nucleotides in a FASTQ file, visualize quality scores with block characters, and automatically detect adapter sequences.`,
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) > 0 {
			renderFASTQ(args[0])
		} else {
			renderFASTQ("-")
		}
	},
}

func init() {
	rootCmd.AddCommand(fastqCmd)
}

func renderFASTQ(filename string) {
	var reader io.Reader

	if filename == "" || filename == "-" {
		reader = os.Stdin
	} else if strings.HasSuffix(filename, ".gz") {
		file, err := os.Open(filename)
		if err != nil {
			fmt.Println("Error opening file:", err)
			return
		}
		defer file.Close()

		gzipReader, err := gzip.NewReader(file)
		if err != nil {
			fmt.Println("Error opening gzip file:", err)
			return
		}
		defer gzipReader.Close()
		reader = gzipReader
	} else {
		file, err := os.Open(filename)
		if err != nil {
			fmt.Println("Error opening file:", err)
			return
		}
		defer file.Close()
		reader = file
	}

	// Handle interrupt signals
	interruptChan := make(chan os.Signal, 1)
	signal.Notify(interruptChan, syscall.SIGINT)

	scanner := bufio.NewScanner(reader)
	lineCount := 0
	continueProcessing := true
	readName := ""

	go func() {
		<-interruptChan
		fmt.Println("\nReceived interrupt. Finishing the current line...")
		continueProcessing = false
	}()

	for scanner.Scan() && continueProcessing {
		line := scanner.Text()
		lineCount++

		switch lineCount % 4 {
		case 1: // Sequence ID line
			readName = line[1:] // Store the read name without the '@'
		case 2: // Sequence line
			adapterInfo, adapterPos := findAdapterWithMismatch(line, 5, 0.05)
			if adapterInfo != "" {
				// Append adapter name to read name
				readName += fmt.Sprintf("    (%s)", adapterInfo)
			}
			tml.Printf("<italic>%s</italic>\n", readName) // Print the sequence ID with adapter name appended
			fmt.Println(colorizeSequenceWithAdapters(line, adapterPos))
		case 3: // "+" line
			// Skip the + line, do nothing
		case 0: // Quality score line
			fmt.Println(visualizeQuality(line))
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading file:", err)
	}
}

func colorizeSequenceWithAdapters(sequence string, adapterPos []int) string {
	var sb strings.Builder

	if adapterPos != nil {
		// Non-adapter region: Color normally
		nonAdapterSeq := sequence[:adapterPos[0]]
		sb.WriteString(colorizeSequence(nonAdapterSeq))

		// Adapter region: Color the adapter and the rest of the sequence in gray
		adapterSeq := sequence[adapterPos[0]:]
		sb.WriteString(tml.Sprintf("<bg-black><darkgrey>%s</darkgrey></bg-black>", adapterSeq))
	} else {
		// No adapter detected, color entire sequence normally
		sb.WriteString(colorizeSequence(sequence))
	}

	return sb.String()
}

func findAdapterWithMismatch(sequence string, minLength int, maxMismatchPercentage float64) (string, []int) {
	bestMatchPos := -1
	bestMatchLength := 0
	bestAdapterName := ""
	allowedMismatchPercentage := maxMismatchPercentage // maximum allowed mismatch percentage

	// Iterate over all known adapter sequences
	for adapterSeq, adapterName := range adapterSequences {
		adapterLen := len(adapterSeq)

		// Only search for adapters near the end of the sequence
		for i := len(sequence) - minLength; i >= 0; i-- {
			// Calculate how much of the adapter can match starting at this position
			overlapLen := len(sequence) - i
			if overlapLen > adapterLen {
				overlapLen = adapterLen
			}

			if overlapLen < minLength {
				continue // Skip if the overlap is smaller than the minimum required length
			}

			candidate := sequence[i : i+overlapLen]
			mismatches := mismatches(candidate, adapterSeq[:overlapLen])
			mismatchPercentage := float64(mismatches) / float64(overlapLen)

			// Check if the mismatch percentage is below the allowed threshold
			if mismatchPercentage <= allowedMismatchPercentage {
				if bestMatchPos == -1 || (i == bestMatchPos && overlapLen > bestMatchLength) {
					bestMatchPos = i
					bestMatchLength = overlapLen
					bestAdapterName = adapterName
				}
			}
		}
	}

	// Only return matches if the length is greater than minLength and mismatches are below the threshold
	if bestMatchPos != -1 && bestMatchLength >= minLength {
		return bestAdapterName, []int{bestMatchPos, len(sequence)} // Mark the region from the match to the end
	}

	return "", nil // No adapter found
}

// mismatches counts the number of mismatched characters between two strings
func mismatches(seq1, seq2 string) int {
	mismatches := 0
	for i := 0; i < len(seq1); i++ {
		if seq1[i] != seq2[i] {
			mismatches++
		}
	}
	return mismatches
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
