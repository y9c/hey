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
	"sync/atomic"
	"unicode/utf8"

	"github.com/liamg/tml"
	"github.com/spf13/cobra"
)

var (
	fastqMaxRecords int
	fastqCompactLen int
)

var adapterDict = map[string]string{
	"AGATCGGAAGAGCACACGTC":  "TruSeq P7",
	"GATCGGAAGAGCACACGTCT":  "TruSeq P7(-1)",
	"TGGAATTCTCGGGTGCCAAG":  "3' RNA (legacy)",
	"AGATCGGAAGAGCGTCGTGT":  "TruSeq P5",
	"GATCGTCGGACTGTAGAACT":  "5' RNA P5",
	"CTGTCTCTTATACACATCT":   "Tn5 ME",
}

var fastqCmd = &cobra.Command{
	Use:   "fastq [filename]",
	Short: "Colorize and visualize FASTQ",
	Long: `Colorize nucleotides, visualize quality with colored blocks, and detect adapters.

Output:
  - Bases: A=red, T=green, G=yellow, C=blue
  - Quality bar: green(≥30), yellow(20-29), red(10-19), grey(<10)
  - Per-read quality stats: avgQ[min..max] appended to quality line
  - Adapter region highlighted with black background

Options:
  -n limit   Show only first N records (default: unlimited)
  -c compact Truncate sequences longer than this width (default: 80; 0=off)`,
	Args: cobra.MaximumNArgs(1),
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
	fastqCmd.Flags().IntVarP(&fastqMaxRecords, "max-records", "n", 0, "Limit to first N records (0=unlimited)")
	fastqCmd.Flags().IntVarP(&fastqCompactLen, "compact", "c", 80, "Truncate reads longer than this length (0=off)")
}

type readQualStats struct {
	sum   int64
	min   int
	max   int
	count int
}

func (s *readQualStats) avg() float64 {
	if s.count == 0 {
		return 0
	}
	return float64(s.sum) / float64(s.count)
}

type fastqStats struct {
	totalRecords   int
	adapterRecords int
	adapterHits    map[string]int
	totalLen       int64
	totalQual      int64
	baseQualCount  int64
}

func (s *fastqStats) avgQuality() float64 {
	if s.baseQualCount == 0 {
		return 0
	}
	return float64(s.totalQual) / float64(s.baseQualCount)
}

func (s *fastqStats) avgLength() float64 {
	if s.totalRecords == 0 {
		return 0
	}
	return float64(s.totalLen) / float64(s.totalRecords)
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

		gz, err := gzip.NewReader(file)
		if err != nil {
			fmt.Println("Error opening gzip file:", err)
			return
		}
		defer gz.Close()
		reader = gz
	} else {
		file, err := os.Open(filename)
		if err != nil {
			fmt.Println("Error opening file:", err)
			return
		}
		defer file.Close()
		reader = file
	}

	interruptChan := make(chan os.Signal, 1)
	signal.Notify(interruptChan, syscall.SIGINT)
	defer signal.Stop(interruptChan)

	scanner := bufio.NewScanner(reader)
	buf := make([]byte, 512*1024)
	scanner.Buffer(buf, 10*1024*1024)
	lineCount := 0
	continueProcessing := int32(1)
	readName := ""

	stats := &fastqStats{
		adapterHits: make(map[string]int),
	}

	go func() {
		<-interruptChan
		fmt.Println("\nReceived interrupt. Finishing current record...")
		atomic.StoreInt32(&continueProcessing, 0)
	}()

	for scanner.Scan() && atomic.LoadInt32(&continueProcessing) == 1 {
		line := scanner.Text()
		lineCount++

		switch lineCount % 4 {
		case 1: // Header
			readName = ""
			if len(line) > 1 {
				readName = line[1:]
			}
		case 2: // Sequence
			stats.totalLen += int64(len(line))
			adapterName := ""
			adapterPos := -1
			for _, info := range findAdapterWithMismatch(line, 5, 0.05) {
				adapterName = info.name
				adapterPos = info.pos
				stats.adapterRecords++
				stats.adapterHits[adapterName]++
				break
			}

			printLabel(readName, adapterName, adapterPos, len(line))
			printSequence(line, adapterPos)
		case 3: // "+" (skip)
		case 0: // Quality
			// Compute per-read quality stats
			currQual := &readQualStats{min: math.MaxInt32}
			for i := 0; i < len(line); i++ {
				score := int(line[i]) - 33
				if score < 0 {
					score = 0
				}
				currQual.sum += int64(score)
				currQual.count++
				if score < currQual.min {
					currQual.min = score
				}
				if score > currQual.max {
					currQual.max = score
				}
				stats.totalQual += int64(score)
			}
			if currQual.count == 0 {
				currQual.min = 0
			}
			stats.baseQualCount += int64(len(line))
			printQuality(line, currQual)
			stats.totalRecords++

			if fastqMaxRecords > 0 && stats.totalRecords >= fastqMaxRecords {
				atomic.StoreInt32(&continueProcessing, 0)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading file:", err)
	}

	if stats.totalRecords > 0 {
		printSummary(stats)
	}
}

func printLabel(readName, adapterName string, adapterPos, seqLen int) {
	parts := strings.Fields(readName)
	shortName := readName
	if len(parts) >= 1 {
		shortName = parts[0]
	}

	line := tml.Sprintf("<italic>%s [%d bp]</italic>", shortName, seqLen)

	if adapterName != "" {
		remaining := seqLen - adapterPos
		line += " " + tml.Sprintf("<bold><bg-red>%s +%dnt</bg-red></bold>",
			adapterName, remaining)
	}

	tml.Println(line)
}

func printSequence(seq string, adapterPos int) {
	truncLen := fastqCompactLen

	if adapterPos >= 0 {
		before := seq[:adapterPos]
		after := seq[adapterPos:]

		if truncLen > 0 && utf8.RuneCountInString(before) > truncLen-6 {
			limit := truncLen - 6
			idx := byteAtRune(before, limit)
			fmt.Print(colorizeSeq(before[:idx]))
			fmt.Println(tml.Sprintf(" <grey>...</grey>" +
				"<bg-black><darkgrey>%s</darkgrey></bg-black>", after))
		} else {
			fmt.Print(colorizeSeq(before))
			fmt.Println(tml.Sprintf("<bg-black><darkgrey>%s</darkgrey></bg-black>", after))
		}
	} else {
		displaySeq := seq
		trimmed := false
		if truncLen > 0 && utf8.RuneCountInString(seq) > truncLen {
			idx := byteAtRune(seq, truncLen-3)
			displaySeq = seq[:idx]
			trimmed = true
		}
		fmt.Print(colorizeSeq(displaySeq))
		if trimmed {
			fmt.Println(tml.Sprintf(" <grey>...</grey>"))
		} else {
			fmt.Println()
		}
	}
}

func byteAtRune(s string, n int) int {
	i := 0
	count := 0
	for i < len(s) {
		_, size := utf8.DecodeRuneInString(s[i:])
		if count >= n {
			break
		}
		i += size
		count++
	}
	return i
}

func printQuality(q string, qs *readQualStats) {
	display := q
	trimmed := false
	maxDisplay := fastqCompactLen
	if maxDisplay > 0 && len(q) > maxDisplay-3 {
		display = q[:maxDisplay-3]
		trimmed = true
	}
	fmt.Print(visualizeQuality(display))
	if trimmed {
		fmt.Print(tml.Sprintf(" <grey>...</grey>"))
	}
	fmt.Print(tml.Sprintf(" <grey>Q%.1f[%d..%d]</grey>", qs.avg(), qs.min, qs.max))
	fmt.Println()
}

func colorizeSeq(seq string) string {
	var sb strings.Builder
	sb.Grow(len(seq) * 24)
	for i := 0; i < len(seq); i++ {
		c := seq[i]
		switch c {
		case 'A':
			sb.WriteString(tml.Sprintf("<bg-red>A</bg-red>"))
		case 'T':
			sb.WriteString(tml.Sprintf("<bg-green>T</bg-green>"))
		case 'G':
			sb.WriteString(tml.Sprintf("<bg-yellow>G</bg-yellow>"))
		case 'C':
			sb.WriteString(tml.Sprintf("<bg-blue>C</bg-blue>"))
		case 'N':
			sb.WriteString(tml.Sprintf("<darkgrey>N</darkgrey>"))
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

func visualizeQuality(q string) string {
	var sb strings.Builder
	sb.Grow(len(q) * 24)

	batchColor := ""
	for i := 0; i < len(q); i++ {
		score := int(q[i]) - 33
		if score < 0 {
			score = 0
		}
		ch := blockChar(score)
		newColor := qualityColor(score)

		if newColor != batchColor {
			if i > 0 {
				sb.WriteString(fmt.Sprintf("</%s>", batchColor))
			}
			batchColor = newColor
			sb.WriteString(fmt.Sprintf("<%s>", newColor))
		}
		sb.WriteRune(ch)
	}
	if batchColor != "" {
		sb.WriteString(fmt.Sprintf("</%s>", batchColor))
	}
	return sb.String()
}

func qualityColor(score int) string {
	if score >= 30 {
		return "green"
	} else if score >= 20 {
		return "yellow"
	} else if score >= 10 {
		return "red"
	}
	return "darkgrey"
}

func blockChar(score int) rune {
	if score >= 40 {
		return '│'
	}
	if score >= 30 {
		return '▓'
	}
	if score >= 20 {
		return '▒'
	}
	if score >= 10 {
		return '░'
	}
	return '·'
}

type adapterInfo struct {
	name string
	pos  int
}

func findAdapterWithMismatch(sequence string, minLength int, maxMismatchPercentage float64) []adapterInfo {
	var results []adapterInfo
	bestMatchPos := math.MaxInt
	bestMatchLength := 0
	bestAdapterName := ""

	for adapterSeq, adapterName := range adapterDict {
		adapterLen := len(adapterSeq)

		for i := len(sequence) - minLength; i >= 0; i-- {
			overlapLen := len(sequence) - i
			if overlapLen > adapterLen {
				overlapLen = adapterLen
			}
			if overlapLen < minLength {
				continue
			}

			candidate := sequence[i : i+overlapLen]
			miss := mismatches(candidate, adapterSeq[:overlapLen])
			mismatchPct := float64(miss) / float64(overlapLen)

			if mismatchPct <= maxMismatchPercentage {
				if i < bestMatchPos || (i == bestMatchPos && overlapLen > bestMatchLength) {
					bestMatchPos = i
					bestMatchLength = overlapLen
					bestAdapterName = adapterName
				}
			}
		}
	}

	if bestMatchPos != math.MaxInt && bestMatchLength >= minLength {
		results = append(results, adapterInfo{bestAdapterName, bestMatchPos})
	}
	return results
}

func mismatches(seq1, seq2 string) int {
	count := 0
	for i := 0; i < len(seq1); i++ {
		if seq1[i] != seq2[i] {
			count++
		}
	}
	return count
}

func printSummary(stats *fastqStats) {
	fmt.Println()
	sep := tml.Sprintf(" <blue>%s</blue>", strings.Repeat("─", 50))
	fmt.Println(sep)

	tml.Printf(" <bold>FASTQ Summary</bold>\n")
	tml.Printf(" <blue>Records</blue>     : %d\n", stats.totalRecords)
	tml.Printf(" <blue>Avg Length</blue>  : %.0f bp\n", stats.avgLength())
	tml.Printf(" <blue>Avg Quality</blue> : Q%.1f\n", stats.avgQuality())

	if stats.adapterRecords > 0 {
		pct := float64(stats.adapterRecords) / float64(stats.totalRecords) * 100
		tml.Printf(" <blue>Adapter Hits</blue>: %d (%.1f%%)\n", stats.adapterRecords, pct)
		for name, cnt := range stats.adapterHits {
			p := float64(cnt) / float64(stats.totalRecords) * 100
			tml.Printf("   · %s: %d (%.1f%%)\n", name, cnt, p)
		}
	}
	fmt.Println(sep)
}
