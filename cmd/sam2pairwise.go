package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/liamg/tml"
	"github.com/spf13/cobra"
)

var (
	knownMutation     string
	knownMutationMark string
	filterForward     bool
	filterReverse     bool
	tagKeys           []string // For storing custom tags from -t flag
	qualityCutoff     int      // Quality score cutoff
)

const (
	minIntronCompressLength = 20 // Minimum length of 'N' CIGAR op to compress
	condensedNSEdgeLength   = 5  // Number of Ns/dots on each side of compressed format
)

var sam2pairwiseCmd = &cobra.Command{
	Use:     "sam2pairwise [-m REF>ALT] [-l MARK] [-f] [-r] [-t TAG]...",
	Aliases: []string{"sam", "s2p"}, // Alias added
	Short:   "Convert SAM records from stdin into pairwise alignment format",
	Long: `Processes SAM records, parsing CIGAR and MD tags to generate pairwise alignments.

Highlighting Logic (with -m REF>ALT, e.g., -m C>T):
  - The specific REF>ALT mutation (C>T) is NOT highlighted.
  - The original REF base (C) when it's NOT mutated IS highlighted.
  - Other mutations involving the REF base (e.g., C>A, C>G) ARE highlighted.
  - All other mismatches not involving the specified REF base ARE highlighted.
  - Matches not involving the specified REF base are NOT highlighted.

Highlighting Logic (without -m):
  - All mismatches are highlighted with a colored background.
  - Matches are plain text.

Filtering Options:
  -f, --forward: Only process Read 1 Forward or Read 2 Reverse reads.
  -r, --reverse: Only process Read 1 Reverse or Read 2 Forward reads.
  (If neither -f nor -r is specified, all reads are processed).

Marking Mismatches:
  Optionally, use -m REF>ALT (e.g., -m C>T) and -l MARK (e.g., -l '.')
  to mark specific known mismatches with MARK instead of a space.

Output Tags:
  Use -t TAG (e.g., -t NM -t XA) to specify which SAM tags to display in the
  information line. If -t is not used, the MD tag is shown by default.
  Multiple -t flags can be used.

Long Intron Formatting (>20 Ns):
  Introns (N operations) longer than 20 bases are condensed in the output:
  Ref:   <darkgrey>NNNNN..[count]nt...NNNNN</darkgrey>
  Query: <darkgrey>..... ..[count]nt... .....</darkgrey>
  Marker:        [spaces matching width]`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(knownMutationMark) > 1 {
			fmt.Fprintln(os.Stderr, "Error: -l mark must be a single character.")
			os.Exit(1)
		}
		if knownMutation != "" && (len(knownMutation) != 3 || knownMutation[1] != '>') {
			fmt.Fprintln(os.Stderr, "Error: -m mutation format must be REF>ALT (e.g., C>T).")
			os.Exit(1)
		}
		if filterForward && filterReverse {
			fmt.Fprintln(os.Stderr, "Error: Cannot use -f and -r flags simultaneously.")
			os.Exit(1)
		}
		processSAMStdin()
	},
}

func init() {
	rootCmd.AddCommand(sam2pairwiseCmd)
	sam2pairwiseCmd.Flags().StringVarP(&knownMutation, "mutation", "m", "", "Known mutation to affect highlighting (e.g., C>T)")
	sam2pairwiseCmd.Flags().StringVarP(&knownMutationMark, "mark", "l", ".", "Single character to use for marking the known mutation")
	sam2pairwiseCmd.Flags().BoolVarP(&filterForward, "forward", "f", false, "Filter for Read 1 Forward or Read 2 Reverse")
	sam2pairwiseCmd.Flags().BoolVarP(&filterReverse, "reverse", "r", false, "Filter for Read 1 Reverse or Read 2 Forward")
	sam2pairwiseCmd.Flags().StringSliceVarP(&tagKeys, "tag", "t", []string{"MD"}, "Tag(s) to show in the name line (default MD). Can be used multiple times.")
	sam2pairwiseCmd.Flags().IntVarP(&qualityCutoff, "quality-cutoff", "q", 0, "Quality score cutoff for highlighting bases (default 0, disabled)")
}

func processSAMStdin() {
	interruptChan := make(chan os.Signal, 1)
	signal.Notify(interruptChan, syscall.SIGINT, syscall.SIGTERM)
	continueProcessing := true

	go func() {
		<-interruptChan
		continueProcessing = false
	}()

	scanner := bufio.NewScanner(os.Stdin)

	var knownRefBase, knownAltBase byte
	useKnownMutation := false
	if knownMutation != "" {
		knownRefBase = knownMutation[0]
		knownAltBase = knownMutation[2]
		useKnownMutation = true
	}
	markChar := '.'
	if len(knownMutationMark) > 0 {
		markChar = []rune(knownMutationMark)[0]
	}

	for continueProcessing && scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "@") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 11 {
			if continueProcessing {
				fmt.Fprintln(os.Stderr, "Skipping invalid SAM record (less than 11 fields):", line)
			}
			continue
		}

		readName := fields[0]
		flagStr := fields[1]
		refName := fields[2]
		pos := fields[3]
		cigar := fields[5]
		seq := fields[9]
		qual := fields[10]

		var outputTagValues []string // To store the values of the requested tags for the info line

		// Extract specified tags for the info line
		for _, requestedTagKey := range tagKeys {
			foundTagValue := "" // Default to empty string if tag not found
			for _, field := range fields[11:] {
				// SAM tags are typically TAG:TYPE:VALUE
				parts := strings.SplitN(field, ":", 3)
				if len(parts) == 3 && parts[0] == requestedTagKey {
					foundTagValue = parts[2] // The value part
					break
				}
			}
			outputTagValues = append(outputTagValues, foundTagValue)
		}
		outputTagsString := strings.Join(outputTagValues, "|") // Join multiple tag values with a semicolon

		// Apply filtering based on flags
		if filterForward || filterReverse {
			flag, err := strconv.Atoi(flagStr)
			if err != nil {
				if continueProcessing {
					fmt.Fprintf(os.Stderr, "Skipping invalid SAM record (invalid flag %s): %s\n", flagStr, line)
				}
				continue
			}

			isPaired := (flag & 0x1) != 0
			isRead1 := (flag & 0x40) != 0
			isRead2 := (flag & 0x80) != 0
			isReverse := (flag & 0x10) != 0

			if filterForward {
				if !isPaired {
					if isReverse {
						continue
					}
				} else {
					if !((isRead1 && !isReverse) || (isRead2 && isReverse)) {
						continue
					}
				}
			} else if filterReverse {
				if !isPaired {
					if !isReverse {
						continue
					}
				} else {
					if !((isRead1 && isReverse) || (isRead2 && !isReverse)) {
						continue
					}
				}
			}
		}

		// Extract MD tag specifically for samToPairwise function, as its logic depends on it.
		mdTagForAlignment := ""
		for _, field := range fields[11:] {
			if strings.HasPrefix(field, "MD:Z:") {
				mdTagForAlignment = field[5:]
				break
			}
		}

		refSeq, alignedSeq, markers, err := samToPairwise(seq, qual, qualityCutoff, cigar, mdTagForAlignment, useKnownMutation, knownRefBase, knownAltBase, markChar)
		if err != nil {
			if continueProcessing {
				// Suppress error for potentially truncated final lines if interrupted
				// fmt.Fprintf(os.Stderr, "Error processing read %s: %v\n", readName, err)
			}
			continue
		}

		if continueProcessing {
			// tml.Printf("<darkgrey><italic>%s\t%s\t%s\t%s\t%s\t%s</italic></darkgrey>\n", readName, flagStr, refName, pos, cigar, outputTagsString)
			tml.Printf("<darkgrey><italic>%s %s %s %s %s %s</italic></darkgrey>\n", readName, flagStr, refName, pos, cigar, outputTagsString)
			tml.Printf(alignedSeq + "\n")
			fmt.Println(markers)
			tml.Printf(refSeq + "\n")
			fmt.Println()
		}
	}

	if err := scanner.Err(); err != nil {
		if continueProcessing {
			fmt.Fprintln(os.Stderr, "Error reading standard input:", err)
		}
	}

	if !continueProcessing {
		tml.Fprintf(os.Stderr, "<yellow><bold>\nSignal received. Finishing current record and exiting.</bold></yellow>\n")
	}
}

// MDTagEntry holds parsed information from an MD tag component.
type MDTagEntry struct {
	Num     int    // Number of matching bases
	Changes string // Reference bases for mismatch or deletion
	IsDel   bool   // True if this entry represents a deletion (^)
}

// parseMDTag breaks down an MD:Z tag string into usable components.
func parseMDTag(mdTag string) ([]MDTagEntry, error) {
	var entries []MDTagEntry
	var numStr strings.Builder
	var changeStr strings.Builder
	state := "num"

	if mdTag == "" {
		return entries, nil
	}

	addNumEntry := func() error {
		if numStr.Len() > 0 {
			num, err := strconv.Atoi(numStr.String())
			if err != nil {
				return fmt.Errorf("internal error: invalid number '%s' in MD tag parse", numStr.String())
			}
			entries = append(entries, MDTagEntry{Num: num})
			numStr.Reset()
		}
		return nil
	}

	addDelEntry := func() error {
		if changeStr.Len() > 0 {
			entries = append(entries, MDTagEntry{Changes: changeStr.String(), IsDel: true})
			changeStr.Reset()
			return nil
		}
		return fmt.Errorf("empty deletion sequence found after '^' in MD tag")
	}

	addMismatchEntry := func() error {
		if changeStr.Len() == 1 {
			entries = append(entries, MDTagEntry{Changes: changeStr.String(), IsDel: false, Num: 0})
			changeStr.Reset()
			return nil
		}
		return fmt.Errorf("invalid mismatch sequence '%s' (must be 1 base) in MD tag", changeStr.String())
	}

	for i, char := range mdTag {
		isLastChar := (i == len(mdTag)-1)

		switch state {
		case "num":
			if char >= '0' && char <= '9' {
				numStr.WriteRune(char)
			} else {
				if err := addNumEntry(); err != nil {
					return nil, err
				}
				if char == '^' {
					state = "del_start"
				} else if (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') {
					changeStr.WriteRune(char)
					state = "change"
				} else {
					return nil, fmt.Errorf("unexpected character '%c' after number in MD tag at position %d", char, i)
				}
			}
		case "change":
			if err := addMismatchEntry(); err != nil {
				return nil, err
			}
			if char >= '0' && char <= '9' {
				numStr.WriteRune(char)
				state = "num"
			} else if char == '^' {
				state = "del_start"
			} else {
				return nil, fmt.Errorf("unexpected character '%c' after mismatch base in MD tag at position %d", char, i)
			}
		case "del_start":
			if (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') {
				changeStr.WriteRune(char)
				state = "del"
			} else {
				return nil, fmt.Errorf("expected base after deletion '^' in MD tag, got '%c' at position %d", char, i)
			}
		case "del":
			if (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') {
				changeStr.WriteRune(char)
			} else {
				if err := addDelEntry(); err != nil {
					return nil, err
				}
				if char >= '0' && char <= '9' {
					numStr.WriteRune(char)
					state = "num"
				} else if char == '^' {
					state = "del_start"
				} else {
					return nil, fmt.Errorf("unexpected character '%c' after deletion sequence in MD tag at position %d", char, i)
				}
			}
		}

		if isLastChar {
			switch state {
			case "num":
				if err := addNumEntry(); err != nil {
					return nil, err
				}
			case "change":
				return nil, fmt.Errorf("MD tag cannot end immediately after a mismatch base")
			case "del":
				if err := addDelEntry(); err != nil {
					return nil, err
				}
			case "del_start":
				return nil, fmt.Errorf("MD tag cannot end with deletion start '^'")
			}
		}
	}
	return entries, nil
}

func samToPairwise(seq string, qual string, qualityCutoff int, cigar string, mdTag string, useKnownMutation bool, knownRefBase byte, knownAltBase byte, markChar rune) (refSeqColored string, alignedSeqColored string, markers string, err error) {
	var refBuilder, alignedSeqBuilder, markerBuilder strings.Builder
	seqPos := 0

	cigarOps, err := parseCigar(cigar)
	if err != nil {
		return "", "", "", fmt.Errorf("error parsing CIGAR '%s': %w", cigar, err)
	}

	var mdEntries []MDTagEntry
	mdIndex := 0
	mdSubPos := 0
	hasMD := mdTag != ""
	mdParseErr := false
	if hasMD {
		mdEntries, err = parseMDTag(mdTag)
		if err != nil {
			hasMD = false
			mdParseErr = true
			mdEntries = nil
		}
	}

	for _, op := range cigarOps {
		length := op.Length
		opType := op.Op

		switch opType {
		case 'M', '=', 'X':
			for range length { // Modernized loop
				if seqPos >= len(seq) {
					return "", "", "", fmt.Errorf("CIGAR M/=/X asks for base %d but sequence length is %d", seqPos+1, len(seq))
				}
				readBase := seq[seqPos]
				var refBase byte = 'N'
				isMismatch := false
				marker := '|'

				if hasMD {
					for mdIndex < len(mdEntries) && mdEntries[mdIndex].Num == 0 && len(mdEntries[mdIndex].Changes) == 0 && !mdEntries[mdIndex].IsDel {
						mdIndex++
					}
					if mdIndex >= len(mdEntries) {
						if !mdParseErr {
							// Suppress warning for cleaner output
						}
						hasMD = false
						refBase = 'N'
						isMismatch = true
					} else {
						currentMdEntry := &mdEntries[mdIndex]
						if currentMdEntry.IsDel {
							return "", "", "", fmt.Errorf("MD tag indicates deletion (^) during CIGAR M/=/X at MD index %d (MD: %s)", mdIndex, mdTag)
						}
						if currentMdEntry.Num > 0 {
							refBase = readBase
							isMismatch = false
							mdSubPos++
							if mdSubPos == currentMdEntry.Num {
								mdIndex++
								mdSubPos = 0
							}
						} else {
							if len(currentMdEntry.Changes) != 1 {
								refBase = 'N'
							} else {
								refBase = currentMdEntry.Changes[0]
							}
							isMismatch = true
							mdIndex++
							mdSubPos = 0
						}
					}
				} else {
					if opType == '=' {
						refBase = readBase
						isMismatch = false
					} else {
						refBase = 'N'
						isMismatch = true
					}
				}

				shouldHighlightRead := false
				shouldHighlightRef := false
				if isMismatch {
					marker = ' '
					if useKnownMutation {
						if refBase == knownRefBase && readBase == knownAltBase {
							marker = markChar
						} else if refBase == knownRefBase {
							shouldHighlightRead = true
							shouldHighlightRef = true
						} else {
							shouldHighlightRead = true
							shouldHighlightRef = true
						}
					} else {
						shouldHighlightRead = true
						shouldHighlightRef = true
					}
				} else {
					marker = '|'
					if useKnownMutation && refBase == knownRefBase {
						shouldHighlightRead = true
						shouldHighlightRef = true
					}
				}
				lowQuality := false
				if qualityCutoff > 0 && seqPos < len(qual) {
					qualityScore := int(qual[seqPos]) - 33
					if qualityScore < qualityCutoff {
						lowQuality = true
					}
				}
				applyColor(&alignedSeqBuilder, readBase, shouldHighlightRead, lowQuality)
				applyColor(&refBuilder, refBase, shouldHighlightRef, false)
				markerBuilder.WriteRune(marker)
				seqPos++
			}

		case 'I':
			for range length { // Modernized loop
				if seqPos >= len(seq) {
					return "", "", "", fmt.Errorf("CIGAR I asks for base %d but sequence length is %d", seqPos+1, len(seq))
				}
				readBase := seq[seqPos]
				lowQuality := false
				if qualityCutoff > 0 && seqPos < len(qual) {
					qualityScore := int(qual[seqPos]) - 33
					if qualityScore < qualityCutoff {
						lowQuality = true
					}
				}
				applyColor(&alignedSeqBuilder, readBase, true, lowQuality)
				applyColor(&refBuilder, '-', true, false)
				markerBuilder.WriteByte(' ')
				seqPos++
			}
		case 'D':
			if !hasMD {
				if !mdParseErr {
					// Suppress warning
				}
				for range length { // Modernized loop
					applyColor(&alignedSeqBuilder, '-', true, false)
					applyColor(&refBuilder, 'N', true, false)
					markerBuilder.WriteByte(' ')
				}
			} else {
				deletedBasesFound := 0
				for deletedBasesFound < length {
					for mdIndex < len(mdEntries) && mdEntries[mdIndex].Num == 0 && len(mdEntries[mdIndex].Changes) == 0 && !mdEntries[mdIndex].IsDel {
						mdIndex++
					}
					if mdIndex >= len(mdEntries) {
						if !mdParseErr {
							// Suppress warning
						}
						for range length - deletedBasesFound { // Modernized loop
							applyColor(&alignedSeqBuilder, '-', true, false)
							applyColor(&refBuilder, 'N', true, false)
							markerBuilder.WriteByte(' ')
						}
						deletedBasesFound = length
						hasMD = false
						break
					}

					currentMdEntry := &mdEntries[mdIndex]
					if currentMdEntry.IsDel {
						delSeqLen := len(currentMdEntry.Changes)
						basesAvailableInMd := delSeqLen - mdSubPos
						basesNeededForCigar := length - deletedBasesFound
						basesToTake := min(basesNeededForCigar, basesAvailableInMd)

						for j := range basesToTake { // Modernized loop
							delBase := currentMdEntry.Changes[mdSubPos+j]
							applyColor(&alignedSeqBuilder, '-', true, false)
							applyColor(&refBuilder, delBase, true, false)
							markerBuilder.WriteByte(' ')
						}
						deletedBasesFound += basesToTake
						mdSubPos += basesToTake
						if mdSubPos == delSeqLen {
							mdIndex++
							mdSubPos = 0
						}
					} else {
						return "", "", "", fmt.Errorf("MD tag indicates match/mismatch (Num: %d, Changes: '%s') during CIGAR D op at MD index %d (MD: %s)", currentMdEntry.Num, currentMdEntry.Changes, mdIndex, mdTag)
					}
				}
			}
		case 'N':
			if length > minIntronCompressLength {
				middleStr := fmt.Sprintf("..%dnt..", length)
				displayWidth := condensedNSEdgeLength*2 + len(middleStr)
				refBuilder.WriteString("<darkgrey>")
				refBuilder.WriteString(strings.Repeat("N", condensedNSEdgeLength))
				refBuilder.WriteString(middleStr)
				refBuilder.WriteString(strings.Repeat("N", condensedNSEdgeLength))
				refBuilder.WriteString("</darkgrey>")
				alignedSeqBuilder.WriteString("<darkgrey>")
				alignedSeqBuilder.WriteString(strings.Repeat(".", condensedNSEdgeLength))
				alignedSeqBuilder.WriteString(middleStr)
				alignedSeqBuilder.WriteString(strings.Repeat(".", condensedNSEdgeLength))
				alignedSeqBuilder.WriteString("</darkgrey>")
				markerBuilder.WriteString(strings.Repeat(" ", displayWidth))
			} else {
				for range length { // Modernized loop
					applyColor(&alignedSeqBuilder, '.', false, false)
					applyColor(&refBuilder, 'N', false, false)
					markerBuilder.WriteByte(' ')
				}
			}
		case 'S':
			for range length { // Modernized loop
				if seqPos >= len(seq) {
					return "", "", "", fmt.Errorf("CIGAR S asks for base %d but sequence length is %d", seqPos+1, len(seq))
				}
				readBase := seq[seqPos]
				lowQuality := false
				if qualityCutoff > 0 && seqPos < len(qual) {
					qualityScore := int(qual[seqPos]) - 33
					if qualityScore < qualityCutoff {
						lowQuality = true
					}
				}
				applyColor(&alignedSeqBuilder, readBase, true, lowQuality)
				applyColor(&refBuilder, '.', false, false)
				markerBuilder.WriteByte(' ')
				seqPos++
			}
		case 'H':
			continue
		case 'P':
			for range length { // Modernized loop
				applyColor(&alignedSeqBuilder, '*', true, false)
				applyColor(&refBuilder, '*', true, false)
				markerBuilder.WriteByte(' ')
			}
		default:
			return "", "", "", fmt.Errorf("unsupported CIGAR operation: %c", opType)
		}
	}

	expectedSeqLenConsumed := 0
	for _, op := range cigarOps {
		if strings.ContainsRune("MIS=X", op.Op) {
			expectedSeqLenConsumed += op.Length
		}
	}
	if seqPos != expectedSeqLenConsumed && len(seq) > 0 {
		// Suppress warning
	}

	return refBuilder.String(), alignedSeqBuilder.String(), markerBuilder.String(), nil
}

func applyColor(builder *strings.Builder, base byte, shouldHighlight bool, lowQuality bool) {
	if lowQuality {
		builder.WriteString("<cyan>")
		builder.WriteByte(base)
		builder.WriteString("</cyan>")
		return
	}

	color := ""
	if shouldHighlight {
		switch base {
		case 'A', 'a':
			color = "red"
		case 'T', 't':
			color = "green"
		case 'G', 'g':
			color = "yellow"
		case 'C', 'c':
			color = "blue"
		case '-':
			color = "black"
		case '*':
			color = "magenta"
		}
	}

	switch base {
	case 'A', 'a', 'T', 't', 'G', 'g', 'C', 'c':
		if shouldHighlight && color != "" {
			builder.WriteString("<bg-" + color + ">")
			builder.WriteByte(base)
			builder.WriteString("</bg-" + color + ">")
		} else {
			builder.WriteByte(base)
		}
	case '-':
		if shouldHighlight && color == "black" {
			builder.WriteString("<bg-black>")
			builder.WriteByte(base)
			builder.WriteString("</bg-black>")
		} else {
			builder.WriteByte(base)
		}
	case '*':
		if shouldHighlight && color == "magenta" {
			builder.WriteString("<bg-magenta>")
			builder.WriteByte(base)
			builder.WriteString("</bg-magenta>")
		} else {
			builder.WriteByte(base)
		}
	case 'N', 'n':
		builder.WriteString(tml.Sprintf("<darkgrey>%c</darkgrey>", base))
	case '.':
		builder.WriteString(tml.Sprintf("<darkgrey>%c</darkgrey>", base))
	default:
		builder.WriteByte(base)
	}
}

type CigarOp struct {
	Length int
	Op     rune
}

func parseCigar(cigar string) ([]CigarOp, error) {
	if cigar == "*" || cigar == "" {
		return []CigarOp{}, nil
	}
	var ops []CigarOp
	var lengthStr strings.Builder
	for i, char := range cigar {
		if char >= '0' && char <= '9' {
			lengthStr.WriteRune(char)
		} else if strings.ContainsRune("MIDNSHP=X", char) {
			if lengthStr.Len() == 0 {
				return nil, fmt.Errorf("CIGAR operation '%c' at index %d has no preceding length", char, i)
			}
			length, err := strconv.Atoi(lengthStr.String())
			if err != nil {
				return nil, fmt.Errorf("invalid CIGAR length format '%s' before op '%c': %w", lengthStr.String(), char, err)
			}
			if length <= 0 {
				return nil, fmt.Errorf("invalid non-positive CIGAR length %d for operation '%c'", length, char)
			}
			ops = append(ops, CigarOp{Length: length, Op: char})
			lengthStr.Reset()
		} else {
			return nil, fmt.Errorf("invalid character '%c' in CIGAR string at index %d", char, i)
		}
	}
	if lengthStr.Len() > 0 {
		return nil, fmt.Errorf("CIGAR string ends with an incomplete operation (number '%s' without type)", lengthStr.String())
	}
	return ops, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
