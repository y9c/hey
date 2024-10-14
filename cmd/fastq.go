package cmd

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/liamg/tml"
	"github.com/spf13/cobra"
)

// Adapter sequences (extracted from the C++ code you provided)
var adapterSequences = []string{
	"GATCGGAAGAGCTCGTATGCCGTCTTCTGCTTG",                               // Illumina Single End Adapter 1
	"CAAGCAGAAGACGGCATACGAGCTCTTCCGATCT",                              // Illumina Single End Adapter 2
	"AATGATACGGCGACCACCGAGATCTACACTCTTTCCCTACACGACGCTCTTCCGATCT",      // Illumina Single End PCR Primer 1
	"GATCGGAAGAGCGGTTCAGCAGGAATGCCGAG",                                // Illumina Paired End Adapter 2
	"CAAGCAGAAGACGGCATACGAGATCGGTCTCGGCATTCCTGCTGAACCGCTCTTCCGATCT",   // Illumina Paired End PCR Primer 2
	"ACACTCTTTCCCTACACGACGCTCTTCCGATCT",                               // Illumina Small RNA Sequencing Primer
	"ACAGGTTCAGAGTTCTACAGTCCGAC",                                      // Illumina DpnII expression Adapter 1
	"CAAGCAGAAGACGGCATACGA",                                           // Illumina DpnII expression Adapter 2
	"CGACAGGTTCAGAGTTCTACAGTCCGACGATC",                                // Illumina DpnII expression Sequencing Primer
	"CAAGCAGAAGACGGCATACGA",                                           // Illumina NlaIII expression Adapter 2
	"TGGAATTCTCGGGTGCCAAGG",                                           // Illumina Small RNA Adapter 2
	"GATCGGAAGAGCACACGTCT",                                            // Illumina Multiplexing Adapter 1
	"GTGACTGGAGTTCAGACGTGTGCTCTTCCGATCT",                              // Illumina Multiplexing PCR Primer 2.01
	"CGGTCTCGGCATTCCTGCTGAACCGCTCTTCCGATCT",                           // Illumina Paired End Sequencing Primer 2
	"CTGATCTAGAGGTACCGGATCCCAGCAGT",                                   // ABI Dynabead EcoP Oligo
	"CTGCCCCGGGTTCCTCATTCTCTCAGCAGCATG",                               // ABI Solid3 Adapter A
	"CCACTACGCCTCCGCTTTCCTCTCTATGGGCAGTCGGTGAT",                       // ABI Solid3 Adapter B
	"TGGAATTCTCGGGTGCCAAGG",                                           // Illumina Small RNA Adapter
	"CGACAGGTTCAGAGTTCTACAGTCCGACGATC",                                // Illumina 5p RNA Adapter
	"GATCGGAAGAGCACACGTCTGAACTCCAGTCAC",                               // TruSeq Adapter, Index 1
	"GATCGGAAGAGCACACGTCTGAACTCCAGTCACCGATGTATCTCGTATGCCGTCTTCTGCTTG", // TruSeq Adapter, Index 2
	"GATCGGAAGAGCACACGTCTGAACTCCAGTCACTTAGGCATCTCGTATGCCGTCTTCTGCTTG", // TruSeq Adapter, Index 3
	"GATCGGAAGAGCACACGTCTGAACTCCAGTCACACTTGAATCTCGTATGCCGTCTTCTGCTTG", // TruSeq Adapter, Index 8
	"GATCGGAAGAGCACACGTCTGAACTCCAGTCACGATCAGATCTCGTATGCCGTCTTCTGCTTG", // TruSeq Adapter, Index 9
	"GTGACTGGAGTTCAGACGTGTGCTCTTCCGATCT",                              // TruSeq Multiplexing Read 2 Sequencing Primer
	"ACACTCTTTCCCTACACGACGCTCTTCCGATCT",                               // TruSeq Universal Adapter
	"TGGAATTCTCGGGTGCCAAGG",                                           // Illumina Small RNA 3p Adapter 1
	"GTGACTGGAGTTCAGACGTGTGCTCTTCCGATCT",                              // Illumina Multiplexing Index Sequencing Primer
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
			tml.Printf("<italic>%s</italic>\n", line[1:])
		case 2: // Sequence line
			fmt.Println(colorizeSequenceWithAdapters(line))
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

func colorizeSequenceWithAdapters(sequence string) string {
	adapterRegion := findAdapterWithMismatch(sequence, 0.05)
	var sb strings.Builder

	if adapterRegion != nil {
		// Non-adapter region: Color normally
		nonAdapterSeq := sequence[:adapterRegion[0]]
		sb.WriteString(colorizeSequence(nonAdapterSeq))

		// Adapter region: Color the adapter and the rest of the sequence in gray
		adapterSeq := sequence[adapterRegion[0]:]
		sb.WriteString(tml.Sprintf("<bg-black><darkgrey>%s</darkgrey></bg-black>", adapterSeq))
	} else {
		// No adapter detected, color entire sequence normally
		sb.WriteString(colorizeSequence(sequence))
	}

	return sb.String()
}

// findAdapterWithMismatch finds an adapter with up to `maxMismatchPercentage` allowed mismatches
func findAdapterWithMismatch(sequence string, maxMismatchPercentage float64) []int {
	for _, adapter := range adapterSequences {
		maxMismatches := int(math.Ceil(maxMismatchPercentage * float64(len(adapter))))
		for i := 0; i <= len(sequence)-len(adapter); i++ {
			candidate := sequence[i : i+len(adapter)]
			if mismatches(candidate, adapter) <= maxMismatches {
				return []int{i, len(sequence)} // Extend to the end of the sequence
			}
		}
	}
	return nil
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
