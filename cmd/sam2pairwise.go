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
)

var sam2pairwiseCmd = &cobra.Command{
	Use:   "sam2pairwise [-m REF>ALT] [-l MARK] [-f] [-r]",
	Short: "Convert SAM records from stdin into pairwise alignment format",
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
  to mark specific known mismatches with MARK instead of a space.`,
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
			fmt.Fprintln(os.Stderr, "Skipping invalid SAM record (less than 11 fields):", line)
			continue
		}

		readName := fields[0]
		flagStr := fields[1]
		refName := fields[2]
		pos := fields[3]
		cigar := fields[5]
		seq := fields[9]
		mdTagValue := ""

		if filterForward || filterReverse {
			flag, err := strconv.Atoi(flagStr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Skipping invalid SAM record (invalid flag %s): %s\n", flagStr, line)
				continue
			}

			isRead1 := (flag & 0x40) != 0
			isRead2 := (flag & 0x80) != 0
			isReverse := (flag & 0x10) != 0

			if filterForward {
				if !((isRead1 && !isReverse) || (isRead2 && isReverse)) {
					continue
				}
			} else if filterReverse {
				if !((isRead1 && isReverse) || (isRead2 && !isReverse)) {
					continue
				}
			}
		}

		for _, field := range fields[11:] {
			if strings.HasPrefix(field, "MD:Z:") {
				mdTagValue = field[5:]
				break
			}
		}

		refSeq, alignedSeq, markers, err := samToPairwise(seq, cigar, mdTagValue, useKnownMutation, knownRefBase, knownAltBase, markChar)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error processing read %s: %v\n", readName, err)
			continue
		}

		tml.Printf("<darkgrey><italic>%s\t%s\t%s\t%s\t%s\t%s</italic></darkgrey>\n", readName, flagStr, refName, pos, cigar, mdTagValue)
		tml.Printf(alignedSeq + "\n")
		fmt.Println(markers)
		tml.Printf(refSeq + "\n")
		fmt.Println()
	}

	if err := scanner.Err(); err != nil {
		if continueProcessing {
			fmt.Fprintln(os.Stderr, "Error reading standard input:", err)
		}
	}

	if !continueProcessing {
		tml.Fprintf(os.Stderr, "<yellow><bold>Signal received. Finishing current record and exiting...</bold></yellow>\n")
	}
}

type MDTagEntry struct {
	Num     int
	Changes string
	IsDel   bool
}

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
				return fmt.Errorf("invalid number '%s' in MD tag", numStr.String())
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
		return fmt.Errorf("empty deletion sequence found in MD tag")
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
				return nil, fmt.Errorf("MD tag cannot end after a mismatch base")
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

func samToPairwise(seq string, cigar string, mdTag string, useKnownMutation bool, knownRefBase byte, knownAltBase byte, markChar rune) (refSeqColored string, alignedSeqColored string, markers string, err error) {
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
			fmt.Fprintf(os.Stderr, "Warning: Error parsing MD tag '%s', proceeding without MD info: %v\n", mdTag, err)
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
			for i := 0; i < length; i++ {
				if seqPos >= len(seq) {
					return "", "", "", fmt.Errorf("CIGAR asks for base beyond sequence length (M/=/X op)")
				}
				readBase := seq[seqPos]
				var refBase byte = readBase
				isMismatch := false
				marker := '|' // Default marker for match

				if hasMD {
					for hasMD && mdIndex < len(mdEntries) && mdEntries[mdIndex].Num == 0 && len(mdEntries[mdIndex].Changes) == 0 && !mdEntries[mdIndex].IsDel {
						mdIndex++
					}
					if mdIndex >= len(mdEntries) {
						if !mdParseErr {
							fmt.Fprintf(os.Stderr, "Warning: Reached end of MD tag prematurely during M/=/X op%s. Assuming 'N' for reference base.\n",
								func() string {
									if mdTag != "" {
										return fmt.Sprintf(" (MD: %s)", mdTag)
									}
									return ""
								}())
						}
						hasMD = false
					}

					if hasMD {
						currentMdEntry := &mdEntries[mdIndex]
						if currentMdEntry.IsDel {
							return "", "", "", fmt.Errorf("MD tag indicates deletion (^) during CIGAR match/mismatch (M/=/X) operation at MD index %d (MD: %s)", mdIndex, mdTag)
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
								fmt.Fprintf(os.Stderr, "Warning: Internal logic error or invalid MD? Mismatch entry has len != 1 ('%s') in MD tag '%s'. Treating reference base as 'N'.\n", currentMdEntry.Changes, mdTag)
								refBase = 'N'
							} else {
								refBase = currentMdEntry.Changes[0]
							}
							isMismatch = true
							mdIndex++
							mdSubPos = 0
						}
					} else {
						refBase = 'N'
						isMismatch = true
					}
				} else if opType == 'X' || (opType == 'M' && !hasMD) {
					refBase = 'N'
					isMismatch = true
				}

				// --- New Highlighting Logic Determination ---
				shouldHighlightRead := false
				shouldHighlightRef := false

				if isMismatch {
					marker = ' ' // Default marker for mismatch
					if useKnownMutation {
						if refBase == knownRefBase && readBase == knownAltBase {
							// This is the specific known mutation - DO NOT highlight
							shouldHighlightRead = false
							shouldHighlightRef = false
							marker = markChar // Use the specific marker
						} else if refBase == knownRefBase {
							// Other mutation of the known ref base - DO highlight
							shouldHighlightRead = true
							shouldHighlightRef = true
						} else {
							// Mismatch not involving the known ref base - DO highlight
							shouldHighlightRead = true
							shouldHighlightRef = true
						}
					} else {
						// Not using known mutation, highlight all mismatches
						shouldHighlightRead = true
						shouldHighlightRef = true
					}
				} else { // Is a Match
					marker = '|'
					if useKnownMutation && refBase == knownRefBase {
						// Match involves the known ref base - DO highlight
						shouldHighlightRead = true
						shouldHighlightRef = true
					} else {
						// Normal match - DO NOT highlight
						shouldHighlightRead = false
						shouldHighlightRef = false
					}
				}
				// --- End New Highlighting Logic ---

				applyColor(&alignedSeqBuilder, readBase, shouldHighlightRead)
				applyColor(&refBuilder, refBase, shouldHighlightRef)
				markerBuilder.WriteRune(marker)

				seqPos++
			}

		case 'I':
			for i := 0; i < length; i++ {
				if seqPos >= len(seq) {
					return "", "", "", fmt.Errorf("CIGAR asks for base beyond sequence length (I op)")
				}
				readBase := seq[seqPos]
				// Insertions are always highlighted as a difference
				applyColor(&alignedSeqBuilder, readBase, true)
				applyColor(&refBuilder, '-', true)
				markerBuilder.WriteByte(' ')
				seqPos++
			}
		case 'D':
			if !hasMD {
				if !mdParseErr {
					fmt.Fprintf(os.Stderr, "Warning: Deletion (D) in CIGAR but no valid MD tag. Representing deleted reference bases as 'N'.\n")
				}
				for i := 0; i < length; i++ {
					// Deletions are always highlighted
					applyColor(&alignedSeqBuilder, '-', true)
					applyColor(&refBuilder, 'N', true)
					markerBuilder.WriteByte(' ')
				}
			} else {
				deletedBasesFound := 0
				for deletedBasesFound < length {
					for hasMD && mdIndex < len(mdEntries) && mdEntries[mdIndex].Num == 0 && len(mdEntries[mdIndex].Changes) == 0 && !mdEntries[mdIndex].IsDel {
						mdIndex++
					}
					if mdIndex >= len(mdEntries) {
						if !mdParseErr {
							fmt.Fprintf(os.Stderr, "Warning: Reached end of MD tag prematurely during D op%s. Representing remaining deleted bases as 'N'.\n",
								func() string {
									if mdTag != "" {
										return fmt.Sprintf(" (MD: %s)", mdTag)
									}
									return ""
								}())
						}
						for k := 0; k < length-deletedBasesFound; k++ {
							applyColor(&alignedSeqBuilder, '-', true)
							applyColor(&refBuilder, 'N', true)
							markerBuilder.WriteByte(' ')
						}
						deletedBasesFound = length
						hasMD = false
						break
					}

					currentMdEntry := &mdEntries[mdIndex]
					if currentMdEntry.IsDel {
						delSeqLen := len(currentMdEntry.Changes)
						basesAvailable := delSeqLen - mdSubPos
						basesNeeded := length - deletedBasesFound
						basesToTake := min(basesNeeded, basesAvailable)
						for j := 0; j < basesToTake; j++ {
							delBase := currentMdEntry.Changes[mdSubPos+j]
							applyColor(&alignedSeqBuilder, '-', true)
							// Highlight the specific deleted base from reference
							applyColor(&refBuilder, delBase, true)
							markerBuilder.WriteByte(' ')
						}
						deletedBasesFound += basesToTake
						mdSubPos += basesToTake
						if mdSubPos == delSeqLen {
							mdIndex++
							mdSubPos = 0
						}
					} else {
						return "", "", "", fmt.Errorf("MD tag indicates match/mismatch (Num: %d, Changes: '%s') during CIGAR deletion (D) operation at MD index %d (MD: %s)", currentMdEntry.Num, currentMdEntry.Changes, mdIndex, mdTag)
					}
				}
			}
		case 'N':
			for i := 0; i < length; i++ {
				applyColor(&alignedSeqBuilder, '.', false) // Skipped regions are not highlighted
				applyColor(&refBuilder, 'N', false)
				markerBuilder.WriteByte(' ')
			}
		case 'S':
			for i := 0; i < length; i++ {
				if seqPos >= len(seq) {
					return "", "", "", fmt.Errorf("CIGAR asks for base beyond sequence length (S op)")
				}
				readBase := seq[seqPos]
				// Soft clipped bases are highlighted
				applyColor(&alignedSeqBuilder, readBase, true)
				applyColor(&refBuilder, 'N', false) // Corresponding ref part is N, not highlighted
				markerBuilder.WriteByte(' ')
				seqPos++
			}
		case 'H':
			continue // Hard clipping does nothing to alignment output
		case 'P':
			for i := 0; i < length; i++ {
				// Padding is highlighted
				applyColor(&alignedSeqBuilder, '*', true)
				applyColor(&refBuilder, '*', true)
				markerBuilder.WriteByte(' ')
			}
		default:
			return "", "", "", fmt.Errorf("unsupported CIGAR operation: %c", opType)
		}
	}

	expectedSeqLen := 0
	for _, op := range cigarOps {
		if strings.ContainsRune("MIS=X", op.Op) {
			expectedSeqLen += op.Length
		}
	}
	if seqPos != len(seq) && len(seq) > 0 {
		if seqPos < len(seq) && !mdParseErr {
			fmt.Fprintf(os.Stderr, "Warning: CIGAR operations consumed %d bases, but sequence length is %d. Result might be truncated or CIGAR/SEQ inconsistent.\n", seqPos, len(seq))
		} else if seqPos > len(seq) {
			fmt.Fprintf(os.Stderr, "Error: CIGAR operations consumed %d bases, but sequence length is only %d. CIGAR/SEQ inconsistent.\n", seqPos, len(seq))
		}
	}

	return refBuilder.String(), alignedSeqBuilder.String(), markerBuilder.String(), nil
}

// applyColor applies tml background color tags if shouldHighlight is true.
func applyColor(builder *strings.Builder, base byte, shouldHighlight bool) {
	tagOpen := ""
	tagClose := ""
	color := ""

	// Determine base color only if highlighting is needed
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
			color = "black" // Gap color
		case '*':
			color = "magenta" // Padding color
		}
	}

	// Apply formatting
	switch base {
	case 'A', 'a', 'T', 't', 'G', 'g', 'C', 'c':
		if shouldHighlight && color != "" {
			tagOpen = "<bg-" + color + ">"
			tagClose = "</bg-" + color + ">"
			builder.WriteString(tagOpen)
			builder.WriteByte(base)
			builder.WriteString(tagClose)
		} else { // No highlight or base doesn't have a specific color
			builder.WriteByte(base)
		}
	case '-': // Gap
		if shouldHighlight { // Gaps are usually highlighted
			builder.WriteString(tml.Sprintf("<bg-black>%c</bg-black>", base))
		} else {
			builder.WriteByte(base)
		}
	case '*': // Padding
		if shouldHighlight { // Padding is usually highlighted
			builder.WriteString(tml.Sprintf("<bg-magenta>%c</bg-magenta>", base))
		} else {
			builder.WriteByte(base)
		}
	case 'N', 'n': // Ambiguous
		builder.WriteString(tml.Sprintf("<darkgrey>%c</darkgrey>", base)) // Always grey
	case '.': // Skipped Read
		builder.WriteString(tml.Sprintf("<darkgrey>%c</darkgrey>", base)) // Always grey
	default: // Other characters
		builder.WriteByte(base)
	}
}

type CigarOp struct {
	Length int
	Op     rune
}

func parseCigar(cigar string) ([]CigarOp, error) {
	if cigar == "*" {
		return []CigarOp{}, nil
	}
	var ops []CigarOp
	var lengthStr strings.Builder

	for _, char := range cigar {
		if char >= '0' && char <= '9' {
			lengthStr.WriteRune(char)
		} else if strings.ContainsRune("MIDNSHP=X", char) {
			if lengthStr.Len() == 0 {
				return nil, fmt.Errorf("CIGAR operation '%c' has no preceding length", char)
			}
			length, err := strconv.Atoi(lengthStr.String())
			if err != nil || length <= 0 {
				return nil, fmt.Errorf("invalid CIGAR length '%s' for operation '%c'", lengthStr.String(), char)
			}
			ops = append(ops, CigarOp{Length: length, Op: char})
			lengthStr.Reset()
		} else {
			return nil, fmt.Errorf("invalid character '%c' in CIGAR string", char)
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
